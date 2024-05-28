package proxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

const (
	identifierLength = 32 // Length of the md5sum
	blockRsyncPort   = 3222
)

type ProxyServer struct {
	listenPort     int    // Port to listen on
	blockrsyncPath string // Path to blockrsync binary
	blockSize      int    // Block size to use
	log            logr.Logger
	identifiers    []string
	wg             sync.WaitGroup
}

func NewProxyServer(blockrsyncPath string, blockSize, listenPort int, identifiers []string, logger logr.Logger) *ProxyServer {
	return &ProxyServer{
		listenPort:     listenPort,
		blockrsyncPath: blockrsyncPath,
		log:            logger,
		identifiers:    identifiers,
		blockSize:      blockSize,
	}
}

func (b *ProxyServer) StartServer() error {
	for _, identifier := range b.identifiers {
		if len(identifier) != identifierLength {
			return fmt.Errorf("identifier must be %d characters", identifierLength)
		}
	}
	b.log.Info("Listening:", "host", "localhost", "port", b.listenPort)
	// Create a listener on the desired port
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", b.listenPort))
	if err != nil {
		log.Fatal(err)
	}
	mu := &sync.Mutex{}
	processingMap := make(map[string]int)

	for i := 1; i <= len(b.identifiers); i++ {
		b.wg.Add(1)
		go b.processConnection(listener, processingMap, mu, i)
	}
	b.wg.Wait()
	return nil
}

func (b *ProxyServer) processConnection(listener net.Listener, processing map[string]int, mu *sync.Mutex, i int) {
	keepTrying := true
	for keepTrying {
		b.log.Info("Waiting for connection")
		// Accept incoming connections
		conn, err := listener.Accept()
		if err != nil {
			b.log.Error(err, "Unable to accept connection")
		}
		file, header, err := b.getTargetFileFromIdentifier(conn)
		if err != nil {
			b.log.Error(err, "Unable to get target file from identifier")
			conn.Close()
		}
		mu.Lock()
		if processing[header] > 0 {
			// Someone else is processing same header, ignore this connection
			b.log.Info("other thread is processing header", "thread", processing[header], "header", header)
			mu.Unlock()
			conn.Close()
			continue
		} else {
			b.log.Info("processing header", "header", header, "thread", i)
			processing[header] = i
			mu.Unlock()
		}

		b.log.Info("Accepted connection, starting blockrsync server", "port", blockRsyncPort+i)
		err = b.startsBlockrsyncServer(conn, file, blockRsyncPort+i)
		if err != nil {
			b.log.Error(err, "Unable to start blockrsync server")
		} else {
			b.wg.Done()
			keepTrying = false
		}
	}
}

func (b *ProxyServer) getTargetFileFromIdentifier(conn net.Conn) (string, string, error) {
	header := make([]byte, identifierLength)
	n, err := io.ReadFull(conn, header)
	if err != nil {
		return "", "", err
	}
	if n != identifierLength {
		return "", "", fmt.Errorf("expected %d bytes, got %d", identifierLength, n)
	}
	file := os.Getenv(string(header))
	if file == "" {
		file = os.Getenv((fmt.Sprintf("id-%s", header)))
		if file == "" {
			return "", "", fmt.Errorf("no filepath found for %s", string(header))
		}
	}
	return file, string(header), nil
}

func (b *ProxyServer) startsBlockrsyncServer(rw io.ReadWriteCloser, file string, port int) error {
	defer rw.Close()

	b.log.Info("writing to file", "file", file)
	go b.forkProcess(file, port)

	notConnect := true
	var blockRsyncConn net.Conn
	var err error
	for notConnect {
		b.log.Info("Connecting to blockrsync server", "port", port)
		blockRsyncConn, err = net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
		if err != nil {
			b.log.Info("Waiting to connect to blockrsync server", "error", err)
			time.Sleep(1 * time.Second)
		} else {
			b.log.Info("Connected to blockrsync server")
			notConnect = false
		}
	}
	go func() {
		_, err = io.Copy(rw, blockRsyncConn)
		if err != nil {
			b.log.Error(err, "Unable to copy data from server to client")
		}
	}()
	b.log.Info("Copying data")
	_, err = io.Copy(blockRsyncConn, rw)
	if err != nil {
		b.log.Error(err, "Unable to copy data from client to server")
		return err
	}

	b.log.Info("Successfully completed sync proxy")
	return nil
}

func (b *ProxyServer) forkProcess(file string, port int) {
	arguments := []string{
		file,
		"--target",
		"--port",
		strconv.Itoa(port),
		"--zap-log-level",
		"3",
		"--block-size",
		strconv.Itoa(b.blockSize),
	}

	b.log.Info("Starting blockrsync server", "arguments", arguments)
	cmd := exec.Command(b.blockrsyncPath, arguments...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the command
	err := cmd.Start()
	if err != nil {
		b.log.Error(err, "Unable to start blockrsync server")
	}
	// Wait for the command to finish
	err = cmd.Wait()
	if err != nil {
		b.log.Error(err, "Waiting for blockrsync server to complete")
	}
}
