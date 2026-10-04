package ichorgo

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	ngSectionHeader         = 0x0a0d0d0a
	ngInterface             = 1
	ngPacket                = 2 // obsolete Packet Block
	ngSimplePacket          = 3
	ngEnhancedPacket        = 6
	ngByteOrderMagic uint32 = 0x1a2b3c4d
)

// checkPcapng walks the blocks of a pcapng file before gopacket reads it: gopacket allocates
// each packet from the length the block declares, so a crafted file of a few bytes could
// make it allocate GiBs (an out-of-memory abort, not a recoverable panic). Every packet
// must fit in its block. A truncated last block ends the walk quietly, as it ends reading.
func checkPcapng(r io.Reader) error {
	br := bufio.NewReaderSize(r, 64<<10)
	hdr := make([]byte, 24)

	var (
		order     binary.ByteOrder = binary.LittleEndian
		snaplen   uint32
		haveIface bool
	)

	for {
		if _, err := io.ReadFull(br, hdr[:8]); err != nil {
			return truncatedOK(err)
		}

		read := 8

		// The section header's type reads the same in both byte orders; its byte-order magic
		// sets the order of everything up to the next section.
		if binary.LittleEndian.Uint32(hdr[0:4]) == ngSectionHeader {
			if _, err := io.ReadFull(br, hdr[8:12]); err != nil {
				return truncatedOK(err)
			}

			switch ngByteOrderMagic {
			case binary.LittleEndian.Uint32(hdr[8:12]):
				order = binary.LittleEndian
			case binary.BigEndian.Uint32(hdr[8:12]):
				order = binary.BigEndian
			default:
				return errors.New("invalid pcapng section header")
			}

			read, haveIface = 12, false
		}

		typ, length := order.Uint32(hdr[0:4]), order.Uint32(hdr[4:8])
		if length < 12 || length%4 != 0 || int64(length) < int64(read) {
			return fmt.Errorf("invalid pcapng block length %d", length)
		}

		need := map[uint32]int{ngInterface: 16, ngPacket: 24, ngEnhancedPacket: 24, ngSimplePacket: 12}[typ]
		if need > read {
			if int64(length) < int64(need) {
				return fmt.Errorf("pcapng block of type %d too short", typ)
			}

			if _, err := io.ReadFull(br, hdr[read:need]); err != nil {
				return truncatedOK(err)
			}

			read = need
		}

		if err := checkNgBlock(typ, length, hdr, order, &snaplen, &haveIface); err != nil {
			return err
		}

		if _, err := br.Discard(int(length) - read); err != nil {
			return truncatedOK(err)
		}
	}
}

// checkNgBlock checks that the packet a block declares fits in it (header, fields, trailer).
func checkNgBlock(typ, length uint32, hdr []byte, order binary.ByteOrder, snaplen *uint32, haveIface *bool) error {
	switch typ {
	case ngInterface:
		// gopacket caps simple packets with the first interface's snaplen.
		if !*haveIface {
			*snaplen, *haveIface = order.Uint32(hdr[12:16]), true
		}
	case ngPacket, ngEnhancedPacket:
		if uint64(order.Uint32(hdr[20:24]))+32 > uint64(length) {
			return errors.New("pcapng packet larger than its block")
		}
	case ngSimplePacket:
		captured := order.Uint32(hdr[8:12])
		if *snaplen != 0 && captured > *snaplen {
			captured = *snaplen
		}

		if uint64(captured)+16 > uint64(length) {
			return errors.New("pcapng packet larger than its block")
		}
	}

	return nil
}

func truncatedOK(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}

	return err
}
