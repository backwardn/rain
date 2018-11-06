package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
)

type CompactPeer struct {
	IP   [net.IPv4len]byte
	Port uint16
}

func NewCompactPeer(addr *net.TCPAddr) CompactPeer {
	p := CompactPeer{Port: uint16(addr.Port)}
	copy(p.IP[:], addr.IP)
	return p
}

func (p CompactPeer) Addr() *net.TCPAddr {
	return &net.TCPAddr{IP: p.IP[:], Port: int(p.Port)}
}

func (p CompactPeer) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 6))
	return buf.Bytes(), binary.Write(buf, binary.BigEndian, p)
}

func (p *CompactPeer) UnmarshalBinary(data []byte) error {
	if len(data) != 6 {
		return errors.New("invalid compact peer length")
	}
	return binary.Read(bytes.NewReader(data), binary.BigEndian, p)
}

func DecodePeersCompact(b []byte) ([]*net.TCPAddr, error) {
	if len(b)%6 != 0 {
		return nil, errors.New("invalid peer list length")
	}
	count := len(b) / 6
	addrs := make([]*net.TCPAddr, 0, count)
	for i := 0; i < len(b); i += 6 {
		var peer CompactPeer
		err := peer.UnmarshalBinary(b[i : i+6])
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, peer.Addr())
	}
	return addrs, nil
}
