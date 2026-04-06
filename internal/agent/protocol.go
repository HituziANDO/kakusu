package agent

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
)

type Request struct {
	Type string `json:"type"`
	Key  string `json:"key,omitempty"`
	Salt string `json:"salt,omitempty"`
}

type Response struct {
	Status           string `json:"status"`
	Key              string `json:"key,omitempty"`
	Salt             string `json:"salt,omitempty"`
	HasKey           bool   `json:"has_key,omitempty"`
	TTLSeconds       int    `json:"ttl_seconds,omitempty"`
	RemainingSeconds int    `json:"remaining_seconds,omitempty"`
}

func SendMsg(conn net.Conn, msg any) error {
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

func RecvMsg(conn net.Conn, msg any) error {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return err
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length > 1<<20 {
		return errors.New("message too large")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return err
	}
	return json.Unmarshal(data, msg)
}
