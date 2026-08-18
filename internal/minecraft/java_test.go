package minecraft

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
)

func TestPingJavaSendsValidStatusHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	errs := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errs <- err
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		length, err := readVarInt(r)
		if err != nil {
			errs <- err
			return
		}
		packet := make([]byte, length)
		if _, err := io.ReadFull(r, packet); err != nil {
			errs <- err
			return
		}
		fields := bytes.NewReader(packet)
		if id, err := fields.ReadByte(); err != nil || id != 0 {
			errs <- fmt.Errorf("handshake packet ID = %d, err = %v", id, err)
			return
		}
		if protocol, err := readVarInt(fields); err != nil || protocol != 765 {
			errs <- fmt.Errorf("protocol = %d, err = %v", protocol, err)
			return
		}
		if _, err := readVarInt(r); err != nil { // STATUS request packet length
			errs <- err
			return
		}
		if id, err := r.ReadByte(); err != nil || id != 0 {
			errs <- fmt.Errorf("status request packet ID = %d, err = %v", id, err)
			return
		}
		body := []byte(`{"players":{"online":2,"max":20}}`)
		response := append([]byte{0}, varInt(len(body))...)
		response = append(response, body...)
		_, err = conn.Write(append(varInt(len(response)), response...))
		errs <- err
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	result := PingJava("127.0.0.1", port, time.Second)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if result.Outcome != domain.Success || result.PlayersOnline != 2 || result.PlayersMax != 20 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
