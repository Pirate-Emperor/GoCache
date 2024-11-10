package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

var activeConfig map[config]string
var storedKeys map[string]Value
var activeReplicaConn []RedisConn

// streamKey => Tid => Sequence => []stream
var sd StreamData = StreamData{streams: make(map[string]map[uint64]map[uint64][]stream), tIndexes: []uint64{}, sIndexes: make(map[uint64][]uint64)}

func main() {
	activeConfig = proccessArgs()
	keys, err := loadRedisDB(activeConfig[DIR], activeConfig[DBFILENAME])
	if err != nil {
		log.Printf("error while restoring db: %v, skipped restoration", err.Error())
		storedKeys = make(map[string]Value)
	}
	storedKeys = keys
	activeReplicaConn = []RedisConn{}

	l, err := net.Listen("tcp", "0.0.0.0:"+activeConfig[PORT])
	if err != nil {
		fmt.Println("Failed to bind to port", activeConfig[PORT])
		os.Exit(1)
	}

	if infoMap[REPLICATION]["role"] == "slave" {
		replicaConfig := strings.Split(activeConfig[ReplicaOf], " ")
		url := replicaConfig[0] + ":" + replicaConfig[1]
		go handleReplicaConnection(url, activeConfig[PORT])
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			os.Exit(1)
		}
		go handleConn(RedisConn{conn})
	}
}
