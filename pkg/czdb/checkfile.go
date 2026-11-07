package czdb

import "encoding/binary"

const (
	// hyperHeaderFixedSize is the fixed part of the HyperHeaderBlock:
	// 4B version + 4B clientId + 4B encryptedBlockSize.
	hyperHeaderFixedSize = 12

	// maxEncryptedBlockSize mirrors the sanity limit the search library
	// applies to the encrypted block size.
	maxEncryptedBlockSize = 1_000_000

	// superBlockSize is 1B dbType + 4x4B integers.
	superBlockSize = 17

	// headerBlockEntrySize is 16B start IP + 4B index pointer.
	headerBlockEntrySize = 20

	// indexBlockOverhead is the 4B data pointer + 1B data length that follow
	// the two IPs of an index block.
	indexBlockOverhead = 5
)

// CheckFile reports whether data has the structure of a CZDB database. It only
// looks at the unencrypted part of the file (no key is needed), which is enough
// to reject an HTML error page or a truncated download. Any database family
// (IPv4 or IPv6) is accepted; use CheckFileFor to also check the family.
func CheckFile(data []byte) bool {
	_, _, ok := findSuperBlock(data)
	return ok
}

// CheckFileFor reports whether data is a structurally valid CZDB database of
// the requested IP family (ipv6 false means IPv4).
//
// The family matters because a database of the wrong family still parses fine
// but answers every query of the other family with an error that looks like a
// plain miss, so a mixed up file would silently disable a whole address family.
func CheckFileFor(data []byte, ipv6 bool) bool {
	_, dbType, ok := findSuperBlock(data)
	if !ok {
		return false
	}
	want := byte(0)
	if ipv6 {
		want = 1
	}
	return dbType == want
}

// findSuperBlock locates the SuperBlock of data and returns its offset together
// with the database type byte stored in it.
//
// The size of the random padding between the encrypted block and the SuperBlock
// can only be known by decrypting the former, so every candidate position is
// tested; a valid file matches exactly one of them.
func findSuperBlock(data []byte) (offset int, dbType byte, ok bool) {
	if len(data) < hyperHeaderFixedSize+superBlockSize {
		return 0, 0, false
	}

	encryptedBlockSize := int(binary.LittleEndian.Uint32(data[8:12]))
	if encryptedBlockSize <= 0 || encryptedBlockSize > maxEncryptedBlockSize {
		return 0, 0, false
	}

	base := hyperHeaderFixedSize + encryptedBlockSize
	if len(data)-base < superBlockSize {
		return 0, 0, false
	}
	for candidate := base; candidate+superBlockSize <= len(data); candidate++ {
		if checkSuperBlock(data, candidate) {
			return candidate, data[candidate], true
		}
	}
	return 0, 0, false
}

// checkSuperBlock validates the SuperBlock at offset together with the pointers
// it holds. dbSize must account for exactly the rest of the file, which is the
// strongest available signal that offset is the real start of the database
// body rather than a byte inside the random padding.
func checkSuperBlock(data []byte, offset int) bool {
	block := data[offset : offset+superBlockSize]

	dbType := block[0]
	if dbType > 1 {
		return false
	}

	dbSize := int64(int32(binary.LittleEndian.Uint32(block[1:5])))
	startIndexPtr := int64(int32(binary.LittleEndian.Uint32(block[5:9])))
	headerBlockSize := int64(int32(binary.LittleEndian.Uint32(block[9:13])))
	endIndexPtr := int64(int32(binary.LittleEndian.Uint32(block[13:17])))

	if dbSize != int64(len(data)-offset) {
		return false
	}
	if headerBlockSize <= 0 || headerBlockSize%headerBlockEntrySize != 0 {
		return false
	}
	if headerBlockSize+superBlockSize > dbSize {
		return false
	}
	if startIndexPtr < 0 || endIndexPtr <= startIndexPtr {
		return false
	}

	ipLength := int64(4)
	if dbType == 1 {
		ipLength = 16
	}
	indexBlockLength := ipLength*2 + indexBlockOverhead
	return endIndexPtr+indexBlockLength <= dbSize
}
