package main

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

func ParseArg(arg config) (string, bool) {
	argIndex := slices.Index(os.Args, string("--"+arg))
	if argIndex == -1 {
		return "", false
	}
	if len(os.Args) < argIndex+2 {
		panic("--" + arg + " does not have any parameters passed!")
	}
	return os.Args[argIndex+1], true
}

func getValueFromDB(key string) string {
	val := storedKeys[key]
	exp := val.Exp
	// updatedAt := val.UpdatedAt
	if exp == 0 {
		return val.Data
		// conn.Write(Encode(val.Data, BULK_STRING))
	} else {
		currentTime := time.Now().UnixMilli()
		if currentTime > int64(exp) {
			delete(storedKeys, key)
			return ""
			// conn.Write(Encode("", BULK_STRING))
		} else {
			return val.Data
			// conn.Write(Encode(val.Data, BULK_STRING))
		}
	}
}

func setValueToDB(args []string) bool {
	key := args[0]
	val := Value{}
	withPxArg := false
	if strings.ToLower(args[len(args)-2]) == "px" {
		exp, err := strconv.Atoi(args[len(args)-1])
		if err == nil {
			val.Exp = miliseconds(time.Now().UnixMilli() + int64(exp))
			val.Data = strings.Join(args[1:len(args)-2], " ")
			// val.UpdatedAt = time.Duration(time.Now().UnixMilli())
			withPxArg = true
		}
	}
	if !withPxArg {
		val.Data = strings.Join(args[1:], " ")
	}
	storedKeys[key] = val
	return true
}
