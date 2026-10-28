package coretest

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sslRequestCode = 80877103
	maxFrame       = 1 << 20
	pingQuery      = "-- ping"
)

// newRejectingPool returns a pool on a loopback server that completes the
// PostgreSQL handshake and accepts the empty ping query, but answers every
// other query with an error. Core pings its pool at startup, so Database:false
// needs a pool that answers the ping and nothing else.
func newRejectingPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("coretest: listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go acceptLoop(listener)

	tcpAddr, isTCP := listener.Addr().(*net.TCPAddr)
	if !isTCP {
		t.Fatalf("coretest: listener is not TCP")
	}
	url := "postgres://coretest@127.0.0.1:" + strconv.Itoa(tcpAddr.Port) + "/coretest?sslmode=disable"
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("coretest: parse pool url: %v", err)
	}
	cfg.MaxConns = testPoolConns
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("coretest: open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func acceptLoop(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go serveConn(conn)
	}
}

func serveConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if !handshake(conn) {
		return
	}
	// failed is set by an extended-protocol message; PostgreSQL then answers
	// one ErrorResponse and one ReadyForQuery at the next Sync.
	failed := false
	for {
		kind, body, ok := readMessage(conn)
		if !ok || kind == 'X' {
			return
		}
		switch {
		case kind == 'Q' && isPingText(body):
			writeMessage(conn, 'I', nil)
			writeReady(conn)
		case kind == 'Q':
			writeError(conn)
			writeReady(conn)
		case kind == 'S':
			if failed {
				writeError(conn)
			}
			failed = false
			writeReady(conn)
		case kind == 'H':
		default:
			failed = true
		}
	}
}

// isPingText matches the query pgx sends for Conn.Ping.
func isPingText(text []byte) bool {
	trimmed := strings.TrimRight(string(text), "\x00")
	return strings.TrimSpace(trimmed) == pingQuery
}

// handshake reads the startup packet (declining TLS) and accepts it.
func handshake(conn net.Conn) bool {
	for {
		packet, ok := readPacket(conn)
		if !ok {
			return false
		}
		isSSLRequest := len(packet) >= 4 && binary.BigEndian.Uint32(packet) == sslRequestCode
		if isSSLRequest {
			_, err := conn.Write([]byte{'N'})
			if err != nil {
				return false
			}
			continue
		}
		authOK := make([]byte, 4)
		writeMessage(conn, 'R', authOK)
		writeReady(conn)
		return true
	}
}

// readPacket reads one untyped packet (length includes itself) and returns its payload.
func readPacket(conn net.Conn) ([]byte, bool) {
	header := make([]byte, 4)
	_, err := io.ReadFull(conn, header)
	if err != nil {
		return nil, false
	}
	size := int(binary.BigEndian.Uint32(header))
	if size < 4 || size > maxFrame {
		return nil, false
	}
	payload := make([]byte, size-4)
	_, err = io.ReadFull(conn, payload)
	if err != nil {
		return nil, false
	}
	return payload, true
}

func readMessage(conn net.Conn) (byte, []byte, bool) {
	kind := make([]byte, 1)
	_, err := io.ReadFull(conn, kind)
	if err != nil {
		return 0, nil, false
	}
	body, ok := readPacket(conn)
	return kind[0], body, ok
}

func writeMessage(conn net.Conn, kind byte, body []byte) {
	frame := make([]byte, 5, 5+len(body))
	frame[0] = kind
	binary.BigEndian.PutUint32(frame[1:], uint32(4+len(body)))
	frame = append(frame, body...)
	_, _ = conn.Write(frame)
}

func writeReady(conn net.Conn) {
	writeMessage(conn, 'Z', []byte{'I'})
}

// writeError sends an ErrorResponse (SQLSTATE 0A000).
func writeError(conn net.Conn) {
	var body []byte
	fields := []struct {
		code  byte
		value string
	}{
		{'S', "ERROR"},
		{'V', "ERROR"},
		{'C', "0A000"},
		{'M', "coretest: Options.Database is false; set it to true to use the pool"},
	}
	for _, field := range fields {
		body = append(body, field.code)
		body = append(body, field.value...)
		body = append(body, 0)
	}
	body = append(body, 0)
	writeMessage(conn, 'E', body)
}
