package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

type protospecs string

const (
	SIMPLE_STRING    protospecs = "+"
	SIMPLE_ERRORS    protospecs = "-"
	INTEGER          protospecs = ":"
	BULK_STRING      protospecs = "$"
	ARRAYS           protospecs = "*"
	NULLS            protospecs = "_"
	BOOLEANS         protospecs = "#"
	DOUBLES          protospecs = ","
	BIG_NUMBERS      protospecs = "("
	BULK_ERRORS      protospecs = "!"
	VERBATIM_STRINGS protospecs = "="
	MAPS             protospecs = "%"
	SETS             protospecs = "~"
	PUSHES           protospecs = ">"
)

type command string

const (
	UNSUPPORTED command = "na"
	PING        command = "ping"
	ECHO        command = "echo"
	SET         command = "set"
	GET         command = "get"
	CONFIG      command = "config"
	KEYS        command = "keys"
	INFO        command = "info"
	REPLCONF    command = "replconf"
	PSYNC       command = "psync"
	WAIT        command = "wait"
	TYPE        command = "type"
	XADD        command = "xadd"
	XRANGE      command = "xrange"
	XREAD       command = "xread"
	INCR        command = "incr"
	MULTI       command = "multi"
	EXEC        command = "exec"
	DISCARD     command = "discard"
)

type config string

const (
	DIR        config = "dir"
	DBFILENAME config = "dbfilename"
	PORT       config = "port"
	ReplicaOf  config = "replicaof"
)

type miliseconds uint64

type Value struct {
	Data string
	Exp  miliseconds
}

type tablesubsection uint

const (
	DB_START    tablesubsection = 254
	DB_METADATA tablesubsection = 250
)

type infoSection string

const (
	REPLICATION infoSection = "replication"
)

var infoMap = map[infoSection]map[string]string{
	REPLICATION: {
		"role":               "master",
		"master_replid":      "8371b4fb1155b71f4a04d3e1bc3e18c4a990aeeb",
		"master_repl_offset": "0",
	},
}

type StreamId struct {
	Tid      uint64
	Sequence uint64
	HasTid   bool
	HasSid   bool
}

type TransactionResult struct {
	Value any
	Error string
}

var SupportedInfoSections = []infoSection{REPLICATION}
var SupportedCommands = []command{PING, ECHO, SET, GET, CONFIG, KEYS, INFO, REPLCONF, PSYNC, WAIT, TYPE, XADD, XRANGE, XREAD, INCR, MULTI, EXEC, DISCARD}
var SupportedConfigs = []config{DIR, DBFILENAME, PORT, ReplicaOf}

var defaultConfig = map[config]string{DIR: "/root/codecrafters-redis-go", DBFILENAME: "dump.rdb", PORT: "6379"}

func proccessArgs() map[config]string {
	configs := defaultConfig
	for _, conf := range SupportedConfigs {
		configVal, found := ParseArg(conf)
		if found {
			configs[conf] = configVal
		}
		if conf == ReplicaOf && configVal != "" {
			infoMap[REPLICATION]["role"] = "slave"
		}
	}
	return configs
}

func decodeLengthEncodedData(buff *bytes.Reader) (length int, is_int bool) {
	is_int = false
	lengthByte, _ := buff.ReadByte()
	lengthType := lengthByte >> 6
	if lengthType == 0b00 {
		length = int(lengthByte & 0b00111111)
	} else if lengthType == 0b01 {
		lengthBytes := make([]byte, 2)
		lengthBytes[0] = lengthByte & 0b00111111
		lengthBytes[1], _ = buff.ReadByte()
		length = int(binary.BigEndian.Uint16(lengthBytes))
	} else if lengthType == 0b10 {
		lengthBytes := make([]byte, 4)
		buff.Read(lengthBytes)
		length = int(binary.BigEndian.Uint32(lengthBytes))
	} else {
		is_int = true
		// Special Encoding
		intType := lengthByte & 0b00111111
		if intType == 0 {
			// 8 bit int
			length = 1
		} else if intType == 1 {
			// 16 bit int
			length = 2
			// keyLength = 2
		} else {
			// 32 bit int
			length = 4
		}
	}
	return
}

func loadRedisDB(filepath string, filename string) (storedKeys map[string]Value, err error) {
	MagicNumber := "REDIS0011"
	storedKeys = make(map[string]Value)
	buffer, e := os.ReadFile(path.Join(filepath, filename))
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		err = e
		return
	}
	if len(buffer) == 0 {
		err = errors.New("no data found")
		return
	}
	if len(buffer) < 9 {
		err = errors.New("database buffer is invalid")
		return
	}
	if string(buffer[:9]) != MagicNumber {
		err = errors.New("provided file type is unsupported")
		return
	}

	// Validate Checksum
	fileEndByte := buffer[len(buffer)-9]
	expectedCrc := binary.LittleEndian.Uint64(buffer[len(buffer)-8:])
	if expectedCrc != 0 && fileEndByte == 255 {
		// CRC is provided
		currentCrc := ChecksumJones(buffer[:len(buffer)-8])
		if currentCrc != expectedCrc {
			err = errors.New("rdb checksum verification failed")
			return
		} else {
			log.Printf("Checksum %v matched with provided one\n", expectedCrc)
		}
	} else {
		log.Println("Checksum is not provided. Skipping Checksum verification")
	}

	// TODO: Meatdata Section (FA)

	// Find Database index directly
	databaseIndex := bytes.Index(buffer, []byte{0xFE})
	if databaseIndex == -1 {
		err = errors.New("no table index found")
		return
	}

	// Database Section (FE)
	databaseBuff := bytes.NewReader(buffer[databaseIndex+1:])
	index, e := databaseBuff.ReadByte()
	if e != nil {
		err = e
		return
	}
	log.Printf("Restoring DB with Index %v", index)
	keyValNums := 0
	expiredKeyValNums := 0
	b, e := databaseBuff.ReadByte()
	if e != nil {
		err = e
	}
	if b == 0xFB {
		length, is_int := decodeLengthEncodedData(databaseBuff)
		if is_int {
			// Data is int
			lengthBytes := make([]byte, length)
			_, e = databaseBuff.Read(lengthBytes)
			if e != nil {
				err = e
				return
			}
			if length == 1 {
				keyValNums = int(lengthBytes[0])
			} else if length == 2 {
				keyValNums = int(binary.BigEndian.Uint16(lengthBytes))
			} else {
				keyValNums = int(binary.BigEndian.Uint32(lengthBytes))
			}
		} else {
			keyValNums = length
			// Data is string
		}
		length, is_int = decodeLengthEncodedData(databaseBuff)
		if is_int {
			// Data is int
			lengthBytes := make([]byte, length)
			databaseBuff.Read(lengthBytes)
			if length == 1 {
				expiredKeyValNums = int(lengthBytes[0])
			} else if length == 2 {
				expiredKeyValNums = int(binary.BigEndian.Uint16(lengthBytes))
			} else {
				expiredKeyValNums = int(binary.BigEndian.Uint32(lengthBytes))
			}
		} else {
			expiredKeyValNums = length
			// Data is string
		}
		log.Printf("Total keys with expiry date: %v\n", expiredKeyValNums)
	}
	gotKVp := 0
	for {
		b, e := databaseBuff.ReadByte()
		if e != nil {
			err = e
			return
		}
		key := ""
		val := Value{}
		if b == 0xFC || b == 0xFD {
			// key Has expiry
			if b == 0xFC {
				expBytes := make([]byte, 8)
				_, e := databaseBuff.Read(expBytes)
				if e != nil {
					err = e
					return
				}
				exp := binary.LittleEndian.Uint64(expBytes)
				val.Exp = miliseconds(exp)
			} else {
				expBytes := make([]byte, 4)
				_, e = databaseBuff.Read(expBytes)
				if e != nil {
					err = e
					return
				}
				exp := binary.LittleEndian.Uint32(expBytes)
				val.Exp = miliseconds(exp * 1000)
			}
			b, e = databaseBuff.ReadByte()
			if e != nil {
				err = e
				return
			}
		}
		if b == 0 {
			// String
			keyLength, is_int := decodeLengthEncodedData(databaseBuff)
			if is_int {
				keyBytes := make([]byte, keyLength)
				_, e = databaseBuff.Read(keyBytes)
				if e != nil {
					err = e
					return
				}
				if keyLength == 1 {
					key = fmt.Sprintf("%v", int(keyBytes[0]))
				} else if keyLength == 2 {
					key = fmt.Sprintf("%v", binary.BigEndian.Uint16(keyBytes))
				} else {
					key = fmt.Sprintf("%v", binary.BigEndian.Uint32(keyBytes))
				}
			} else {
				keyBytes := make([]byte, keyLength)
				_, e = databaseBuff.Read(keyBytes)
				if e != nil {
					err = e
					return
				}
				key = string(keyBytes)
			}
			valLength, is_int := decodeLengthEncodedData(databaseBuff)
			if is_int {
				valBytes := make([]byte, valLength)
				_, e = databaseBuff.Read(valBytes)
				if e != nil {
					err = e
					return
				}
				if keyLength == 1 {
					val.Data = fmt.Sprintf("%v", int(valBytes[0]))
				} else if keyLength == 2 {
					val.Data = fmt.Sprintf("%v", binary.BigEndian.Uint16(valBytes))
				} else {
					val.Data = fmt.Sprintf("%v", binary.BigEndian.Uint32(valBytes))
				}
			} else {
				valBytes := make([]byte, valLength)
				_, e = databaseBuff.Read(valBytes)
				if e != nil {
					err = e
					return
				}
				val.Data = string(valBytes)
			}

			// Omit key-value if it is expired
			if val.Exp == 0 || val.Exp > miliseconds(time.Now().UnixMilli()) {
				storedKeys[key] = val
			} else {
				fmt.Println("key with name", key, "is ommited as it was expired")
			}
			gotKVp++
		}
		if b == byte(0xFF) {
			if gotKVp == keyValNums {
				return
			} else {
				log.Println("incomplete Data found, number of keys mistmatch. restore is aborted.")
				storedKeys = make(map[string]Value)
				return
			}
		}
	}
}

type redisCommand struct {
	cmd  command
	args []string
}

func (r *redisCommand) getRawBytesLength() int {
	buff := []string{string(r.cmd)}
	buff = append(buff, r.args...)
	encodedCmd := Encode(buff, ARRAYS)
	return len(encodedCmd)
}

func ParseCommand(buffer []byte) (redisCommand, error) {
	data, err := Decode(buffer)
	if err != nil {
		return redisCommand{}, err
	}
	rcmd := redisCommand{}
	cmds, ok := data.([]string)
	if !ok || len(cmds) < 1 {
		return rcmd, errors.New("command decode failed")
	}
	cmd := command(strings.ToLower(cmds[0]))
	if slices.Contains(SupportedCommands, cmd) {
		rcmd.cmd = cmd
		rcmd.args = cmds[1:]
		return rcmd, nil
	} else {
		rcmd.cmd = UNSUPPORTED
		return rcmd, errors.New("unsupported command passed")
	}
}

// *2\r\n$4\r\nECHO\r\n$3\r\nhey\r\n

func ParseCommands(buffer []byte) ([]redisCommand, error) {
	chunks := strings.Split(string(buffer), "*")
	redisCommands := []redisCommand{}
	for _, chunk := range chunks[1:] {
		if len(chunk) < 3 {
			continue
		}
		encodedBytes := []byte("*")
		data, err := Decode(append(encodedBytes, []byte(chunk)...))
		if err != nil {
			return nil, err
		}
		if len(chunk) < 3 {
			continue
		}
		cmds, ok := data.([]string)
		if !ok || len(cmds) < 1 {
			continue
			// log.Fatal("BadData PCMD")
		}
		commandData := redisCommand{}
		c := command(strings.ToLower(cmds[0]))
		if slices.Contains(SupportedCommands, c) {
			commandData.cmd = c
			if c == KEYS {
				commandData.args = []string{"*"}
			} else {
				commandData.args = cmds[1:]
				if c == REPLCONF && strings.ToLower(cmds[1]) == "getack" {
					commandData.args = []string{cmds[1], "*"}
				}
			}
		} else {
			commandData.cmd = UNSUPPORTED
		}
		redisCommands = append(redisCommands, commandData)
	}
	return redisCommands, nil
}

func Encode(dec any, spec protospecs) []byte {
	switch spec {
	case BULK_STRING:
		decstr, ok := dec.(string)
		if !ok {
			log.Fatalf("Invalid dec passed! expected string got %T", decstr)
		}
		if dec == "" {
			return []byte(string(BULK_STRING) + "-1" + "\r\n")
		}
		return []byte(string(BULK_STRING) + strconv.Itoa(len(decstr)) + "\r\n" + decstr + "\r\n")
	case SIMPLE_STRING:
		decstr, ok := dec.(string)
		if !ok {
			log.Fatalf("Invalid dec passed! expected string got %T", dec)
		}
		return []byte(string(SIMPLE_STRING) + decstr + "\r\n")
	case ARRAYS:
		decarr, ok := dec.([]string)
		if !ok {
			mapArr, ok := dec.([]map[string][]stream)
			// TODO: refactor map to array encoding
			if !ok {
				mapArr, ok := dec.([]map[string][]map[string][]stream)
				if !ok {
					tArr, ok := dec.([]TransactionResult)
					if !ok {
						log.Fatalf("Invalid dec passed! expected array of strings got %T", dec)
					} else {
						// Its mixed types array
						enc := string(ARRAYS) + fmt.Sprintf("%v", len(tArr)) + "\r\n"
						for _, v := range tArr {
							if v.Error != "" {
								enc += string(Encode(v.Error, SIMPLE_ERRORS))
							} else {
								s, ok := v.Value.(int)
								if !ok {
									// Its string
									enc += string(Encode(v.Value, BULK_STRING))
								} else {
									enc += string(Encode(s, INTEGER))
								}
							}
						}
						return []byte(enc)
					}
				} else {
					enc := string(ARRAYS) + strconv.Itoa(len(mapArr)) + "\r\n"
					for _, m := range mapArr {
						for k, v := range m {
							encm := string(ARRAYS) + strconv.Itoa(len(m)*2) + "\r\n"
							// fmt.Println(k, v, "MAP")
							encm += string(Encode(k, BULK_STRING))
							encm += string(Encode(v, ARRAYS))
							enc += encm
						}
					}
					return []byte(enc)
				}
			} else {
				enc := string(ARRAYS) + strconv.Itoa(len(mapArr)) + "\r\n"
				for _, m := range mapArr {
					// fmt.Println(m, "MAP")
					for k, v := range m {
						encm := string(ARRAYS) + strconv.Itoa(len(m)*2) + "\r\n"
						// fmt.Println(k, v, "MAP")
						encm += string(Encode(k, BULK_STRING))
						keyValArr := []string{}
						for _, s := range v {
							keyValArr = append(keyValArr, s.key)
							keyValArr = append(keyValArr, s.value)
						}
						encm += string(Encode(keyValArr, ARRAYS))
						enc += encm
					}
				}
				return []byte(enc)
			}
		}
		enc := string(ARRAYS) + strconv.Itoa(len(decarr)) + "\r\n"
		for _, d := range decarr {
			encBS := Encode(d, BULK_STRING)
			enc += string(encBS)
		}
		return []byte(enc)
	case INTEGER:
		num, ok := dec.(int)
		if !ok {
			log.Fatalf("Invalid dec passed! expected integer got %T", dec)
		}
		numStr := strconv.Itoa(num)
		return []byte(string(INTEGER) + numStr + "\r\n")
	case SIMPLE_ERRORS:
		message, ok := dec.(string)
		if !ok {
			log.Fatalf("inavild dec passed! expected string got %T", dec)
		}
		return []byte(string(SIMPLE_ERRORS) + message + "\r\n")
	default:
		log.Fatal("Yet to implement!")
	}
	return []byte{}
}

func Decode(enc []byte) (any, error) {
	strType := enc[0]
	chunks := bytes.Split(enc[1:], []byte("\r\n"))
	switch protospecs(strType) {
	case SIMPLE_STRING:
		dec := string(chunks[0])
		return dec, nil

	case BULK_STRING:
		_, err := strconv.Atoi(string(chunks[0]))
		if err != nil {
			return nil, err
		}
		_ = chunks[1]
		// if len(content) != contentLength {
		// 	log.Fatal("Content Validation failed! Length mismatch: expacted " + strconv.Itoa(contentLength) + " got " + strconv.Itoa(len(content)))
		// }
		return string(chunks[1]), nil

	case ARRAYS:
		// fmt.Println(string(enc))
		arrayLength, err := strconv.Atoi(string(chunks[0]))
		if err != nil {
			// log.Fatal("ARLEN", err)
			return nil, err
		}
		cursor := 1
		processed := 0
		data := []string{}
		for {
			if protospecs(chunks[cursor][0]) == BULK_STRING {
				clrfBytes := []byte("\r\n")
				encData := chunks[cursor]
				encData = append(encData, clrfBytes...)
				encData = append(encData, chunks[cursor+1]...)
				dec, err := Decode(encData)
				if err != nil {
					return nil, err
				}
				val, ok := dec.(string)
				if ok {
					data = append(data, val)
				} else {
					fmt.Println("BadData Received!: ", dec)
					return nil, errors.New("invalid data found while decoding arrays")
				}
				cursor += 2
				processed++
			}

			if processed == arrayLength {
				break
			}
		}
		return data, nil

	default:
		fmt.Println("Invalid start of data: " + string(enc[0]))
		return "", errors.New("unsupported encoding type found")
	}
}

type RedisConn struct {
	net.Conn
}

func (c RedisConn) SendMessage(message any, rType protospecs) (n int, err error) {
	// c.Write()
	n, err = c.Write(Encode(message, rType))
	return
}

func (c RedisConn) SendError(message string) (n int, err error) {
	n, err = c.Write(Encode(message, SIMPLE_ERRORS))
	return
}

func (c RedisConn) WriteOK() (n int, err error) {
	n, err = c.Write(Encode("OK", SIMPLE_STRING))
	return
}

func handleCommands(c redisCommand, conn *net.Conn, bytesProcessed int) {
	switch c.cmd {
	case PING:
		fmt.Println("Master alive!")
	case SET:
		setValueToDB(c.args)
	case REPLCONF:
		if len(c.args) < 2 {
			fmt.Println(c.cmd, "invalid args passed")
			return
		}
		if strings.ToLower(c.args[0]) == "getack" && c.args[1] == "*" {
			(*conn).Write(Encode([]string{string(REPLCONF), "ACK", strconv.Itoa(bytesProcessed)}, ARRAYS))
		} else {
			fmt.Println(c.cmd, "unupported args passed")
		}
	default:
		fmt.Println(c.cmd, "ignored")
	}
}

func handleReplicaConnection(url string, port string) {
	conn, err := net.Dial("tcp", url)
	bytesProcessed := 0
	if err != nil {
		fmt.Println("Failed to connect to master", url)
		os.Exit(1)
	}
	// Perform Handshak process
	conn.Write(Encode([]string{"PING"}, ARRAYS))
	buff := make([]byte, 512)
	okReceived := 0
	waitingForPSYNC := false
	handshakeCompleted := false
	for {
		size, err := conn.Read(buff)
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Println("[HANDSHAKE] Error reading buffer: ", err.Error())
			os.Exit(1)
		}
		if size == 0 {
			continue
		}
		if string(buff[:size]) == "+PONG\r\n" {
			_, err := conn.Write(Encode([]string{string(REPLCONF), "listening-port", port}, ARRAYS))
			if err != nil {
				log.Fatal("[HANDSHAKE 1] Error writing REPLCONF command")
			}
			_, err = conn.Write(Encode([]string{string(REPLCONF), "capa", "psync2"}, ARRAYS))
			if err != nil {
				log.Fatal("[HANDSHAKE 2] Error writing REPLCONF command")
			}
			continue
		}
		if string(buff[:size]) == "+OK\r\n" {
			okReceived++
		}
		if waitingForPSYNC {
			waitingForPSYNC = false
			handshakeCompleted = true
			// +FULLRESYNC <REPL_ID> 0\r\n
			// RedisDB content + commands (maybe)
			// TODO:Load redis DB
			crlfIndex := bytes.Index(buff[:size], []byte("0\r\n"))
			dbEndByteIndex := bytes.Index(buff[crlfIndex+1:size], []byte{255})
			if dbEndByteIndex != -1 && size > dbEndByteIndex+8 {
				commandsBuff := buff[crlfIndex+1+dbEndByteIndex+8+1 : size]
				cmds, err := ParseCommands(commandsBuff)
				if err != nil {
					fmt.Println(err)
					continue
				}
				if len(cmds) > 0 {
					for _, c := range cmds {
						handleCommands(c, &conn, bytesProcessed)
						bytesProcessed += c.getRawBytesLength()
					}
				}
			}
			continue
		}
		if okReceived == 2 {
			_, err := conn.Write(Encode([]string{string(PSYNC), "?", "-1"}, ARRAYS))
			if err != nil {
				log.Fatal("[HANDSHAKE] Error writing REPLCONF command")
			}
			waitingForPSYNC = true
			okReceived = 0
			continue
		}
		if handshakeCompleted {
			commands, err := ParseCommands(buff[:size])
			if err != nil {
				fmt.Println(err)
				continue
			}
			for _, c := range commands {
				handleCommands(c, &conn, bytesProcessed)
				bytesProcessed += c.getRawBytesLength()
			}
		}
		// +FULLRESYNC
	}
}

func ParseStreamIdFromIdKey(id string) (StreamId, error) {
	sid := StreamId{}
	dashIndex := strings.Index(id, "-")
	if dashIndex != -1 {
		tid, err := strconv.ParseUint(id[:dashIndex], 10, 64)
		if err != nil {
			return sid, err
		} else {
			sid.HasTid = true
			sid.Tid = tid
		}
		seqid, err := strconv.ParseUint(id[dashIndex+1:], 10, 64)
		if err != nil {
			return sid, err
		} else {
			sid.HasSid = true
			sid.Sequence = seqid
		}
	} else {
		tid, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return sid, err
		} else {
			sid.HasTid = true
			sid.Tid = tid
		}
	}
	return sid, nil
}

func initializeStream(streamkey string, tid int, sid int) {
	if sd.streams[streamkey] == nil {
		sd.streams[streamkey] = make(map[uint64]map[uint64][]stream)
	}

	if sd.streams[streamkey][uint64(tid)] == nil {
		sd.streams[streamkey][uint64(tid)] = make(map[uint64][]stream)
	}

	if sd.streams[streamkey][uint64(tid)][uint64(sid)] == nil {
		sd.streams[streamkey][uint64(tid)][uint64(sid)] = []stream{}
	}
}

func IncrementKey(key string) (int, error) {
	value := getValueFromDB(key)
	if value == "" {
		ok := setValueToDB([]string{key, "1"})
		if ok {
			return 1, nil
			// conn.SendMessage(1, INTEGER)
		} else {
			return 0, errors.New("ERR something went wrong while INCR " + key)
			// conn.SendError()
		}
	} else {
		num, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, errors.New("ERR value is not an integer or out of range")
			// conn.SendError("ERR value is not an integer or out of range")
			// continue
		}
		ok := setValueToDB([]string{key, fmt.Sprintf("%v", num+1)})
		if ok {
			return int(num + 1), nil
			// conn.SendMessage(int(num+1), INTEGER)
		} else {
			return 0, errors.New("ERR something went wrong while INCR " + key)
			// conn.SendError("ERR something went wrong while INCR " + key)
		}
	}
}
