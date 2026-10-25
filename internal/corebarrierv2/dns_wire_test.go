package corebarrierv2_test

import (
	"encoding/binary"
	"errors"
	"strings"
)

// Minimal independent DNS wire fixture. It accepts only the single-question
// IN messages used by our real worker/server tests, with bounded compressed
// names. It adds no application or native-core module dependency.
const (
	dnsTypeA    uint16 = 1
	dnsTypeAAAA uint16 = 28
	dnsTypeTXT  uint16 = 16
)

type dnsWireRecord struct {
	kind uint16
	data []byte
}
type dnsWireAnswer struct {
	ID       uint16
	Response bool
	RCode    uint8
	Name     string
	Kind     uint16
	Answers  []dnsWireRecord
}

var errDNSFixture = errors.New("invalid owned-fixture DNS message")

func dnsName(raw []byte, start int) (string, int, error) {
	position, end := start, 0
	var labels []string
	for steps := 0; steps < 128; steps++ {
		if position < 0 || position >= len(raw) {
			return "", 0, errDNSFixture
		}
		n := int(raw[position])
		position++
		if n == 0 {
			if end == 0 {
				end = position
			}
			return strings.Join(labels, ".") + ".", end, nil
		}
		if n&0xc0 == 0xc0 {
			if position >= len(raw) {
				return "", 0, errDNSFixture
			}
			if end == 0 {
				end = position + 1
			}
			position = (n&0x3f)<<8 | int(raw[position])
			continue
		}
		if n > 63 || position+n > len(raw) {
			return "", 0, errDNSFixture
		}
		labels = append(labels, string(raw[position:position+n]))
		position += n
	}
	return "", 0, errDNSFixture
}

func dnsQuestion(raw []byte) (string, uint16, int, error) {
	if len(raw) < 12 || binary.BigEndian.Uint16(raw[4:6]) != 1 {
		return "", 0, 0, errDNSFixture
	}
	name, end, err := dnsName(raw, 12)
	if err != nil || end+4 > len(raw) || binary.BigEndian.Uint16(raw[end+2:end+4]) != 1 {
		return "", 0, 0, errDNSFixture
	}
	return name, binary.BigEndian.Uint16(raw[end : end+2]), end + 4, nil
}

func dnsAnswerData(kind uint16) []byte {
	switch kind {
	case dnsTypeA:
		return []byte{203, 0, 113, 41}
	case dnsTypeAAAA:
		return []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 41}
	case dnsTypeTXT:
		return append([]byte{17}, []byte("owned-darkpro-dns")...)
	default:
		return nil
	}
}

func parseDNSAnswer(raw []byte) (dnsWireAnswer, error) {
	name, kind, end, err := dnsQuestion(raw)
	if err != nil {
		return dnsWireAnswer{}, err
	}
	answer := dnsWireAnswer{ID: binary.BigEndian.Uint16(raw[:2]), Response: raw[2]&0x80 != 0, RCode: raw[3] & 15, Name: name, Kind: kind}
	count := int(binary.BigEndian.Uint16(raw[6:8]))
	if count > 64 {
		return answer, errDNSFixture
	}
	for i := 0; i < count; i++ {
		_, end, err = dnsName(raw, end)
		if err != nil || end+10 > len(raw) {
			return answer, errDNSFixture
		}
		typeCode := binary.BigEndian.Uint16(raw[end : end+2])
		length := int(binary.BigEndian.Uint16(raw[end+8 : end+10]))
		end += 10
		if end+length > len(raw) {
			return answer, errDNSFixture
		}
		answer.Answers = append(answer.Answers, dnsWireRecord{kind: typeCode, data: append([]byte(nil), raw[end:end+length]...)})
		end += length
	}
	return answer, nil
}
