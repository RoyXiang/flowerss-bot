package model

import (
	"encoding/hex"
	"hash/fnv"
)

func genHashID(sLink string, id string) string {
	idString := string(sLink) + "||" + id
	f := fnv.New32()
	f.Write([]byte(idString))

	//encoded := base64.StdEncoding.EncodeToString([]byte(idString))
	encoded := hex.EncodeToString(f.Sum(nil))
	return encoded

}

func getPageOffset(page, limit int) int {
	if page <= 0 || limit <= 0 {
		return -1
	}
	return (page - 1) * limit
}
