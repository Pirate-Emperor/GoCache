package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type stream struct {
	Tid      uint
	Sequence uint
	key      string
	value    string
}

type StreamData struct {
	mu       sync.RWMutex
	streams  map[string]map[uint64]map[uint64][]stream
	tIndexes []uint64
	sIndexes map[uint64][]uint64
}

func handleConn(conn RedisConn) {
	defer conn.Close()
	multiEnabled := false
	commandQueue := []redisCommand{}
	emptyRdbBytes := []byte{
		0x52, 0x45, 0x44, 0x49, 0x53, 0x30, 0x30, 0x31, 0x31, 0xFA, 0x09, 0x72,
		0x65, 0x64, 0x69, 0x73, 0x2D, 0x76, 0x65, 0x72, 0x05, 0x37, 0x2E, 0x32,
		0x2E, 0x30, 0xFA, 0x0A, 0x72, 0x65, 0x64, 0x69, 0x73, 0x2D, 0x62, 0x69,
		0x74, 0x73, 0xC0, 0x40, 0xFA, 0x05, 0x63, 0x74, 0x69, 0x6D, 0x65, 0xC2,
		0x6D, 0x08, 0xBC, 0x65, 0xFA, 0x08, 0x75, 0x73, 0x65, 0x64, 0x2D, 0x6D,
		0x65, 0x6D, 0xC2, 0xB0, 0xC4, 0x10, 0x00, 0xFA, 0x08, 0x61, 0x6F, 0x66,
		0x2D, 0x62, 0x61, 0x73, 0x65, 0xC0, 0x00, 0xFF, 0xF0, 0x6E, 0x3B, 0xFE,
		0xC0, 0xFF, 0x5A, 0xA2}
	for {
		resp := make([]byte, 512)
		n, err := conn.Read(resp)
		if err != nil && err != io.EOF {
			fmt.Println("Error Reading from conn: ", err)
			os.Exit(1)
		}
		if n > 0 {
			if err != nil && err != io.EOF {
				fmt.Println("Error Decode: ", err)
				// os.Exit(1)
			}
			rcmd, err := ParseCommand(resp[:n])
			if err != nil {
				conn.SendError("ERR Invalid command received")
				continue
			}
			// for _, c := range commands {
			args := rcmd.args
			switch rcmd.cmd {
			case PING:
				conn.SendMessage("PONG", SIMPLE_STRING)
			case ECHO:
				msg := strings.Join(args, " ")
				conn.SendMessage(msg, BULK_STRING)
			case SET:
				if multiEnabled {
					commandQueue = append(commandQueue, rcmd)
					conn.SendMessage("QUEUED", BULK_STRING)
				} else {
					setValueToDB(args)
					conn.WriteOK()
					go func() {
						if infoMap[REPLICATION]["role"] == "master" {
							commands := []string{string(SET)}
							commands = append(commands, args...)
							for _, conn := range activeReplicaConn {
								_, err := conn.SendMessage(commands, ARRAYS)
								if err != nil {
									fmt.Println("replica propagation failed!")
								}
							}
						}
					}()
				}
			case GET:
				if multiEnabled {
					commandQueue = append(commandQueue, rcmd)
					conn.SendMessage("QUEUED", BULK_STRING)
				} else {
					value := getValueFromDB(args[0])
					conn.SendMessage(value, BULK_STRING)
				}
			case CONFIG:
				if len(args) == 0 {
					conn.SendError("ERR wrong number of args for `config` command")
					continue
				}
				subcmd := strings.ToLower(args[0])
				if subcmd == "get" {
					if len(args) < 2 {
						conn.SendError("ERR key is required for `config get command`")
						continue
					}
					configName := args[1]
					configVal := activeConfig[config(configName)]
					conn.SendMessage([]string{configName, configVal}, ARRAYS)
				}
			case KEYS:
				if len(args) == 0 {
					conn.SendError("ERR wrong number of args for `keys` command")
					continue
				}
				arg := args[0]
				if arg == "*" {
					keys := []string{}
					for key := range storedKeys {
						keys = append(keys, key)
					}
					conn.SendMessage(keys, ARRAYS)
				}
			case INFO:
				if len(args) == 0 {
					conn.SendError("ERR wrong number of args for `info` command")
					continue
				}
				arg := args[0]
				if arg == string(REPLICATION) {
					infoParts := []string{"# Replication"}
					replInfo := infoMap[REPLICATION]

					for key, val := range replInfo {
						infoParts = append(infoParts, key+":"+val)
					}
					conn.SendMessage(strings.Join(infoParts, "\n"), BULK_STRING)
				} else {
					conn.SendError("ERR only REPLICATION is supported for `info` command as of now")
				}
			case REPLCONF:
				if len(args) < 2 {
					conn.SendError("ERR wrong number of args for `replconf` command")
					continue
				}
				confType := args[0]
				if confType == "listening-port" {
					// fmt.Println("REPL PORT:", args[1])
					conn.WriteOK()
				} else if confType == "capa" {
					// fmt.Println("REPL Capabilities: ", args[1:])
					conn.WriteOK()
				} else {
					conn.SendError("ERR invalid sub command received for `replconf`")
				}

			case PSYNC:
				if len(args) < 2 {
					conn.SendError("ERR wrong number of args for `psync` command")
					continue
				}
				replId := args[0]
				replOffset := args[1]
				resp := []string{"FULLRESYNC"}
				if replId == "?" {
					resp = append(resp, infoMap[REPLICATION]["master_replid"])
				}
				if replOffset == "-1" {
					resp = append(resp, infoMap[REPLICATION]["master_repl_offset"])
				}
				conn.SendMessage(strings.Join(resp, " "), SIMPLE_STRING)
				content := []byte{}
				content = append(content, []byte("$88\r\n")...)
				content = append(content, emptyRdbBytes...)
				if infoMap[REPLICATION]["role"] == "master" {
					activeReplicaConn = append(activeReplicaConn, conn)
				}
				conn.Write(content)

			case WAIT:
				if len(args) < 2 {
					conn.SendError("ERR wrong number of args for `wait` command")
					continue
				}
				conn.SendMessage(len(activeReplicaConn), INTEGER)
			case TYPE:
				if len(args) < 1 {
					conn.SendError("ERR wrong number of args for `type` command")
					continue
				}
				key := args[0]
				value := getValueFromDB(key)
				if value == "" {
					sd.mu.Lock()
					val := sd.streams[key]
					sd.mu.Unlock()
					if len(val) > 0 {
						conn.SendMessage("stream", SIMPLE_STRING)
					} else {
						conn.SendMessage("none", SIMPLE_STRING)
					}
				} else {
					conn.SendMessage("string", SIMPLE_STRING)
				}
			case XADD:
				stramKey := args[0]
				id := args[1]
				keyValueArgs := args[2:]

				// streamId := StreamId{}

				addKeyValuesToStream := func(tid uint64, sid uint64, streamKey string) {
					ns := stream{
						Tid:      uint(tid),
						Sequence: uint(sid),
					}
					for i, kv := range keyValueArgs {
						if i%2 == 0 {
							ns.key = kv
						} else {
							ns.value = kv
							sd.streams[streamKey][uint64(tid)][uint64(sid)] = append(sd.streams[streamKey][uint64(tid)][uint64(sid)], ns)
							ns = stream{Tid: uint(tid), Sequence: uint(sid)}
						}
					}
				}
				if id == "*" {
					// find sequence and value ourself

					// Tid is Current time
					tid := time.Now().UnixMilli()
					sid := 0
					// streamId = StreamId{
					// 	Tid:      uint64(tid),
					// 	HasTid:   true,
					// 	Sequence: uint64(sid),
					// 	HasSid:   true,
					// }
					func() {
						sd.mu.Lock()
						defer sd.mu.Unlock()
						if len(sd.sIndexes[uint64(tid)]) > 0 {
							sid = int(sd.sIndexes[uint64(tid)][len(sd.sIndexes[uint64(tid)])])
						}
						// Taking sid from mapping
						if sid > 0 {
							sid = sid + 1
						}
						initializeStream(stramKey, int(tid), sid)
						addKeyValuesToStream(uint64(tid), uint64(sid), stramKey)
						id = fmt.Sprintf("%v-%v", tid, sid)
						sd.tIndexes = append(sd.tIndexes, uint64(tid))
						// currentTIndex = int(tid)
						if sid == 0 {
							sd.sIndexes[uint64(tid)] = append(sd.sIndexes[uint64(tid)], 1)
							// currentSIndex[uint64(tid)] = 1
						} else {
							sd.sIndexes[uint64(tid)] = append(sd.sIndexes[uint64(tid)], uint64(sid))
							// currentSIndex[uint64(tid)] = sid
						}
						conn.SendMessage(id, BULK_STRING)
					}()
				} else {
					splits := strings.Split(id, "-")
					tidToCheck := splits[0]
					sidToCheck := splits[1]
					if tidToCheck == "0" && sidToCheck == "0" {
						conn.SendError("ERR The ID specified in XADD must be greater than 0-0")
						continue
					}
					var nsidToCheck uint64
					var ntidToCheck uint64
					if ntidToCheck, err = strconv.ParseUint(tidToCheck, 10, 64); err != nil {
						conn.SendError("ERR invalid Id specified in XADD")
						continue
					}

					// currentStreams := streams[stramKey][ntidToCheck]

					if sidToCheck == "*" {
						// Get sid ourself, but tid is given so validate it first
						lastTIndex := 0
						func() {
							sd.mu.Lock()
							defer sd.mu.Unlock()
							if len(sd.tIndexes) > 0 {
								lastTIndex = int(sd.tIndexes[len(sd.tIndexes)-1])
							} else {
								sd.tIndexes = append(sd.tIndexes, 0)
							}
							if ntidToCheck >= uint64(lastTIndex) {
								// TID is higher then or equal to the Last Acknowledged TID So that is OK
								sequence := 0
								if len(sd.sIndexes[ntidToCheck]) > 0 {
									sequence = int(sd.sIndexes[ntidToCheck][len(sd.sIndexes[ntidToCheck])-1]) + 1
								} else {
									if ntidToCheck == 0 {
										sequence = 1
									}
									sd.sIndexes[ntidToCheck] = append(sd.sIndexes[ntidToCheck], uint64(sequence))
								}
								initializeStream(stramKey, int(ntidToCheck), int(nsidToCheck))
								addKeyValuesToStream(ntidToCheck, nsidToCheck, stramKey)
								id = fmt.Sprintf("%v-%v", ntidToCheck, sequence)
								if ntidToCheck != uint64(lastTIndex) {
									sd.tIndexes = append(sd.tIndexes, ntidToCheck)
								}
								// currentTIndex = int(ntidToCheck)
								// if sequence == 0 {
								// 	currentSIndex[uint64(ntidToCheck)] = 1
								// } else {
								// 	currentSIndex[uint64(ntidToCheck)] = sequence
								// }
								conn.SendMessage(id, BULK_STRING)
							} else {
								conn.SendError("ERR The ID specified in XADD is equal or smaller than the target stream top item")
							}
						}()
					} else {
						// Both are given so validate both
						if nsidToCheck, err = strconv.ParseUint(sidToCheck, 10, 64); err != nil {
							conn.SendError("ERR invalid Id specified in XADD")
							continue
						}
						lastTIndex := 0
						func() {
							sd.mu.Lock()
							defer sd.mu.Unlock()
							if len(sd.tIndexes) > 0 {
								lastTIndex = int(sd.tIndexes[len(sd.tIndexes)-1])
							} else {
								sd.tIndexes = append(sd.tIndexes, 0)
							}
							lastSIndex := 0
							if len(sd.sIndexes[ntidToCheck]) > 0 {
								lastSIndex = int(sd.sIndexes[ntidToCheck][len(sd.sIndexes[ntidToCheck])-1])
							}
							if int(lastTIndex) <= int(ntidToCheck) && nsidToCheck > uint64(lastSIndex) {
								initializeStream(stramKey, int(ntidToCheck), int(nsidToCheck))
								addKeyValuesToStream(ntidToCheck, nsidToCheck, stramKey)
								if lastTIndex != int(ntidToCheck) {
									sd.tIndexes = append(sd.tIndexes, ntidToCheck)
								}
								// currentTIndex = int(ntidToCheck)
								if nsidToCheck == 0 {
									sd.sIndexes[ntidToCheck] = append(sd.sIndexes[ntidToCheck], 1)
									// currentSIndex[ntidToCheck] = 1
								} else {
									sd.sIndexes[ntidToCheck] = append(sd.sIndexes[ntidToCheck], nsidToCheck)
									// currentSIndex[ntidToCheck] = nsidToCheck
								}
								conn.SendMessage(id, BULK_STRING)
								// fmt.Println(currentSIndex, currentTIndex)
							} else {
								conn.SendError("ERR The ID specified in XADD is equal or smaller than the target stream top item")
							}
						}()
					}
				}
				// fmt.Println(keyValueArgs)

				// XRANGE some_key 1526985054069 1526985054079
				// 			[
				//   [
				//     "1526985054069-0",
				//     [
				//       "temperature",
				//       "36",
				//       "humidity",
				//       "95"
				//     ]
				//   ],
				//   [
				//     "1526985054079-0",
				//     [
				//       "temperature",
				//       "37",
				//       "humidity",
				//       "94"
				//     ]
				//   ],
				// ]
				// 				[
				//   [
				//     "some_key",
				//     [
				//       [
				//         "1526985054079-0",
				//         [
				//           "temperature",
				//           "37",
				//           "humidity",
				//           "94"
				//         ]
				//       ]
				//     ]
				//   ]
				// ]
			case XRANGE:
				key := args[0]
				start := args[1] // Inclusive
				end := args[2]   // Inclusive
				var (
					startTid int
					startSid int
					endTid   int
					endSid   int
				)
				func() {
					sd.mu.RLock()
					defer sd.mu.RUnlock()
					if start == "-" {
						// Find start ourself
						startTid = int(sd.tIndexes[0])
						startSid = 0
					} else {
						hasStartSequence := strings.Index(start, "-")
						if hasStartSequence == -1 {
							startSid = 0
							stid, err := strconv.ParseUint(start, 10, 64)
							if err != nil {
								conn.SendError("ERR invalid start time received")
								return
							} else {
								startTid = int(stid)
							}
						} else {
							stid, err := strconv.ParseUint(start[:hasStartSequence], 10, 64)
							if err != nil {
								conn.SendError("ERR invalid end time received")
								return
							} else {
								startTid = int(stid)
							}
							stsid, err := strconv.ParseUint(start[hasStartSequence+1:], 10, 64)
							if err != nil {
								conn.SendError("ERR invalid end time received")
								return
							} else {
								startSid = int(stsid)
							}
						}
					}
					if end == "+" {
						// find end ourself
						// fmt.Println(tIndexes)
						endTid = 0
						if len(sd.tIndexes) > 0 {
							endTid = int(sd.tIndexes[len(sd.tIndexes)-1])
						}
						endSid = 0
						if len(sd.sIndexes[uint64(endTid)]) > 0 {
							endSid = int(sd.sIndexes[uint64(endTid)][len(sd.sIndexes[uint64(endTid)])-1])
						}
					} else {
						hasEndSequence := strings.Index(end, "-")
						if hasEndSequence == -1 {
							endSid = 0
							etid, err := strconv.ParseUint(end, 10, 64)
							if err != nil {
								conn.SendError("ERR invalid start time received")
								return
							} else {
								endTid = int(etid)
							}
						} else {
							etid, err := strconv.ParseUint(end[:hasEndSequence], 10, 64)
							if err != nil {
								conn.SendError("ERR invalid end time received")
								return
							} else {
								endTid = int(etid)
							}
							esid, err := strconv.ParseUint(end[hasEndSequence+1:], 10, 64)
							if err != nil {
								conn.SendError("ERR invalid end time received")
								return
							} else {
								endSid = int(esid)
							}
						}
					}
					xRangeArr := []map[string][]stream{}
					tid := startTid
					sid := startSid
					for tid <= endTid && sid <= endSid {
						sKey := fmt.Sprintf("%v-%v", tid, sid)
						currentStrams := sd.streams[key][uint64(tid)][uint64(sid)]
						lastSIndex := 0
						if len(sd.sIndexes[uint64(tid)]) > 0 {
							lastSIndex = int(sd.sIndexes[uint64(tid)][len(sd.sIndexes[uint64(tid)])-1])
						}
						if len(currentStrams) > 0 {
							xRangeArr = append(xRangeArr, map[string][]stream{sKey: currentStrams})
						}
						if lastSIndex > sid {
							sid++
						} else {
							tid++
							sid = 0
						}
					}
					conn.SendMessage(xRangeArr, ARRAYS)
				}()
			case XREAD:
				if len(args) < 2 {
					conn.SendError("ERR insufficient args passed")
					continue
				}
				xRangeArr := []map[string][]map[string][]stream{}
				checkForEntry := func(id string, streamKey string) {
					str, err := ParseStreamIdFromIdKey(id)
					if err != nil {
						conn.SendError("ERR invalid id received for stream " + streamKey)
						return
					}
					endTid := 0
					if len(sd.tIndexes) > 0 {
						endTid = int((sd.tIndexes)[len(sd.tIndexes)-1])
					}
					endSid := 0
					if len((sd.sIndexes)[uint64(endTid)]) > 0 {
						endSid = int((sd.sIndexes)[uint64(endTid)][len((sd.sIndexes)[uint64(endTid)])-1])
					}
					tid := str.Tid
					sid := str.Sequence + 1
					// fmt.Println("GGGG")
					for tid <= uint64(endTid) && sid <= uint64(endSid) {
						// fmt.Println(tid, sid, endTid, endSid)
						sKey := fmt.Sprintf("%v-%v", tid, sid)
						currentStrams := (sd.streams)[streamKey][uint64(tid)][uint64(sid)]
						lastSIndex := 0
						if len((sd.sIndexes)[uint64(tid)]) > 0 {
							lastSIndex = int((sd.sIndexes)[uint64(tid)][len((sd.sIndexes)[uint64(tid)])-1])
						}
						if len(currentStrams) > 0 {
							xRangeArr = append(xRangeArr, map[string][]map[string][]stream{streamKey: {{sKey: currentStrams}}})
						}
						if lastSIndex > int(sid) {
							sid++
						} else {
							tid++
							sid = 0
						}
					}
				}
				if args[0] == "streams" {
					if len(args[1:])%2 != 0 {
						conn.SendError("ERR insufficieant args passed")
						continue
					}
					args := args[1:]
					numKeys := len(args) / 2
					ids := args[numKeys:]

					func() {
						sd.mu.RLock()
						defer sd.mu.RUnlock()

						for i, streamKey := range args[:numKeys] {
							id := ids[i]
							// fmt.Println(id, streamKey)
							checkForEntry(id, streamKey)
						}
						conn.SendMessage(xRangeArr, ARRAYS)
					}()
				} else if args[0] == "block" {
					args := args[1:]
					if len(args) < 4 {
						conn.SendError("ERR insufficient args passed")
						continue
					}
					if args[1] != "streams" {
						conn.SendError("ERR invalid type for block read passed: supported type streams")
						continue
					}
					timer := 0
					timestr, err := strconv.ParseUint(args[0], 10, 64)
					if err != nil {
						conn.SendError("ERR invalid block time passed")
						continue
					} else {
						timer = int(timestr)
					}
					args = args[2:]
					if len(args)%2 != 0 {
						conn.SendError("ERR insufficieant args passed")
						continue
					}
					numKeys := len(args) / 2
					ids := args[numKeys:]

					go func() {
						// Wait for it
						if timer != 0 {
							time.Sleep(time.Duration(timer * int(time.Millisecond)))
							// After send it if exists
							sd.mu.RLock()
							defer sd.mu.RUnlock()

							for i, streamKey := range args[:numKeys] {
								id := ids[i]
								checkForEntry(id, streamKey)
								if len(xRangeArr) == 0 {
									conn.SendMessage("", BULK_STRING)
								} else {
									conn.SendMessage(xRangeArr, ARRAYS)
								}
							}
						} else {
							for i, streamKey := range args[:numKeys] {
								id := ids[i]
								if id == "$" {
									latestTid := 0
									latestSidIndex := 0
									latestTidIndex := len(sd.tIndexes) - 1
									latestSid := 0
									if latestTidIndex != -1 {
										latestTid = int(sd.tIndexes[latestTidIndex])
										latestSidIndex = int(len(sd.sIndexes[uint64(latestTid)])) - 1
									}

									if latestSidIndex == -1 {
										latestSid = 0
									} else {
										latestSid = int(sd.sIndexes[uint64(latestTid)][latestSidIndex])
									}
									id = fmt.Sprintf("%v-%v", latestTid, latestSid)
								}
								for len(xRangeArr) == 0 {
									sd.mu.RLock()
									// fmt.Println(sd.streams)
									// fmt.Println(id)
									checkForEntry(id, streamKey)
									sd.mu.RUnlock()
								}
								conn.SendMessage(xRangeArr, ARRAYS)
							}
						}
					}()
					//
				} else {
					conn.SendError("ERR invalid type passed: supported type streams, block")
				}
			case INCR:
				if len(args) == 0 {
					conn.SendError("ERR insufficient args passed to command INCR")
					continue
				}
				if multiEnabled {
					commandQueue = append(commandQueue, rcmd)
					conn.SendMessage("QUEUED", BULK_STRING)
					continue
				}
				key := args[0]
				newVal, err := IncrementKey(key)
				if err != nil {
					conn.SendError(err.Error())
				} else {
					conn.SendMessage(newVal, INTEGER)
				}

			case MULTI:
				multiEnabled = true
				conn.WriteOK()
			case EXEC:
				if !multiEnabled {
					conn.SendError("ERR EXEC without MULTI")
				} else {
					if len(commandQueue) == 0 {
						conn.SendMessage([]string{}, ARRAYS)
					} else {
						answers := []TransactionResult{}
						// Execute queued commands
						for _, cmd := range commandQueue {
							switch cmd.cmd {
							case SET:
								setValueToDB(cmd.args)
								answers = append(answers, TransactionResult{Value: "OK"})
							case INCR:
								num, err := IncrementKey(cmd.args[0])
								if err != nil {
									answers = append(answers, TransactionResult{Error: err.Error()})
								} else {
									answers = append(answers, TransactionResult{Value: num})
								}
							case GET:
								val := getValueFromDB(cmd.args[0])
								answers = append(answers, TransactionResult{Value: val})
							}
						}
						conn.SendMessage(answers, ARRAYS)
					}
					multiEnabled = false
				}
			case DISCARD:
				if multiEnabled {
					commandQueue = []redisCommand{}
					multiEnabled = false
					conn.WriteOK()
				} else {
					conn.SendError("ERR DISCARD without MULTI")
				}
			default:
				conn.SendError("ERR unsupported Command received")
			}
			// }
		}
	}
}
