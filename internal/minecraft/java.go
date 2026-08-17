// Package minecraft implements the Java Edition Server List Ping (STATUS).
package minecraft

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
	"io"
	"net"
	"time"
)

type status struct {
	Players struct {
		Online int `json:"online"`
		Max    int `json:"max"`
	} `json:"players"`
}

func PingJava(host string, port int, timeout time.Duration) domain.CheckResult {
	now := time.Now()
	result := domain.CheckResult{At: now}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), timeout)
	if err != nil {
		result.Outcome = domain.ConnectTimeout
		result.Detail = err.Error()
		return result
	}
	defer c.Close()
	_ = c.SetDeadline(now.Add(timeout))
	hostBytes := []byte(host)
	packet := append([]byte{0x00, 0x00}, varInt(765)...)
	packet = append(packet, varInt(len(hostBytes))...)
	packet = append(packet, hostBytes...)
	p := make([]byte, 2)
	binary.BigEndian.PutUint16(p, uint16(port))
	packet = append(packet, p...)
	packet = append(packet, 0x01)
	if _, err = c.Write(append(varInt(len(packet)), packet...)); err != nil {
		result.Outcome = domain.ConnectionReset
		result.Detail = err.Error()
		return result
	}
	if _, err = c.Write([]byte{0x01, 0x00}); err != nil {
		result.Outcome = domain.ConnectionReset
		result.Detail = err.Error()
		return result
	}
	r := bufio.NewReader(c)
	if _, err = readVarInt(r); err != nil {
		result.Outcome = domain.StatusTimeout
		result.Detail = err.Error()
		return result
	}
	if _, err = r.ReadByte(); err != nil {
		result.Outcome = domain.InvalidStatusResponse
		return result
	}
	size, err := readVarInt(r)
	if err != nil || size < 1 || size > 1<<20 {
		result.Outcome = domain.InvalidStatusResponse
		return result
	}
	body := make([]byte, size)
	if _, err = io.ReadFull(r, body); err != nil {
		result.Outcome = domain.InvalidStatusResponse
		result.Detail = err.Error()
		return result
	}
	var s status
	if err = json.Unmarshal(body, &s); err != nil {
		result.Outcome = domain.InvalidStatusResponse
		result.Detail = err.Error()
		return result
	}
	result.Outcome = domain.Success
	result.Latency = time.Since(now)
	result.PlayersOnline = s.Players.Online
	result.PlayersMax = s.Players.Max
	return result
}
func varInt(v int) []byte {
	b := []byte{}
	for {
		x := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			x |= 0x80
		}
		b = append(b, x)
		if v == 0 {
			return b
		}
	}
}
func readVarInt(r io.ByteReader) (int, error) {
	n := 0
	for i := 0; i < 5; i++ {
		b, e := r.ReadByte()
		if e != nil {
			return 0, e
		}
		n |= int(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("varint too long")
}
