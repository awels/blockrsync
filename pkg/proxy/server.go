package proxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/go-logr/logr"
)

const (
	md5sumLength = 32 // Length of the md5sum
)

type BlockrsyncServer struct {
	fileSyncTotal  int    // Total number of files to sync
	listenPort     int    // Port to listen on
	blockrsyncPath string // Path to blockrsync binary
	log            logr.Logger
}

func NewBlockrsyncServer(blockrsyncPath string, listenPort, total int, logger logr.Logger) *BlockrsyncServer {
	return &BlockrsyncServer{
		fileSyncTotal:  total,
		listenPort:     listenPort,
		blockrsyncPath: blockrsyncPath,
		log:            logger,
	}
}

func (b *BlockrsyncServer) StartServer() error {
	b.log.Info("Listening:", "host", "localhost", "port", b.listenPort)
	// Create a listener on the desired port
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", b.listenPort))
	if err != nil {
		log.Fatal(err)
	}
	blockRsyncPort := 3222

	for i := 0; i < b.fileSyncTotal; i++ {
		// start fileSyncTotal number of go routines that keep trying until completed.
		go func() {
			keepTrying := true
			for keepTrying {
				b.log.Info("Waiting for connection")
				// Accept incoming connections
				conn, err := listener.Accept()
				if err != nil {
					b.log.Error(err, "Unable to accept connection")
				}
				b.log.Info("Accepted connection, startin blockrsync server", "port", blockRsyncPort)
				err = b.startsBlockrsyncServer(conn, b.blockrsyncPath, blockRsyncPort+i)
				if err != nil {
					b.log.Error(err, "Unable to start blockrsync server")
				} else {
					keepTrying = false
				}
			}
		}()
	}
}

func (b *BlockrsyncServer) startsBlockrsyncServer(conn net.Conn, blockryncPath string, port int) error {
	defer conn.Close()

	header := make([]byte, md5sumLength)
	n, err := io.ReadFull(conn, header)
	if err != nil {
		return err
	}
	if n != md5sumLength {
		return fmt.Errorf("expected %d bytes, got %d", md5sumLength, n)
	}
	file := os.Getenv(string(header))
	if file == "" {
		return fmt.Errorf("no filepath found for %s", string(header))
	}
	b.log.Info("writing to file", "file", file)

	go func() {
		arguments := []string{
			file,
			"--target",
			"--verbose",
			"--no-compress",
			"--port",
			strconv.Itoa(port),
		}

		b.log.Info("Starting blockrsync server", "arguments", arguments)
		cmd := exec.Command(blockryncPath, arguments...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Start the command
		err = cmd.Start()
		if err != nil {
			b.log.Error(err, "Unable to start blockrsync server")
		}
		// Wait for the command to finish
		err = cmd.Wait()
		if err != nil {
			b.log.Error(err, "Waiting for blockrsync server to complete")
		}
	}()

	notConnect := true
	var blockRsyncConn net.Conn
	for notConnect {
		b.log.Info("Connecting to blockrsync server", "port", port)
		blockRsyncConn, err = net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
		if err != nil {
			b.log.Info("Waiting to connect to blockrsync server")
			time.Sleep(1 * time.Second)
		} else {
			b.log.Info("Connected to blockrsync server")
			notConnect = false
		}
	}
	go func() {
		_, err = io.Copy(conn, blockRsyncConn)
		if err != nil {
			b.log.Error(err, "Unable to copy data from server to client")
		}
	}()
	b.log.Info("Copying data")
	_, err = io.Copy(blockRsyncConn, conn)
	if err != nil {
		b.log.Error(err, "Unable to copy data from client to server")
		return err
	}

	b.log.Info("Successfully completed sync proxy")
	return nil
}
