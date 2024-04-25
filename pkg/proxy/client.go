package proxy

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/go-logr/logr"
)

type ProxyClient struct {
	listenPort    int
	targetPort    int
	targetAddress string
	log           logr.Logger
}

func NewProxyClient(listenPort, targetPort int, targetAddress string, logger logr.Logger) *ProxyClient {
	return &ProxyClient{
		listenPort:    listenPort,
		targetPort:    targetPort,
		targetAddress: targetAddress,
		log:           logger,
	}
}

func (b *ProxyClient) ConnectToTarget(identifier string) error {
	if len(identifier) != identifierLength {
		return fmt.Errorf("identifier must be %d characters", identifierLength)
	}
	b.log.Info("Listening:", "host", "localhost", "port", b.listenPort)
	// Create a listener on the desired port
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", b.listenPort))
	if err != nil {
		return err
	}

	// Accept incoming connections
	inConn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer inConn.Close()

	b.log.Info("Connecting to target", "address", b.targetAddress, "port", b.targetPort)
	retry := true
	var outConn net.Conn
	retryCount := 0
	for retry {
		outConn, err = net.Dial("tcp", fmt.Sprintf("%s:%d", b.targetAddress, b.targetPort))
		retry = err != nil
		if err != nil {
			b.log.Error(err, "Unable to connect to target")
		}
		if retry {
			retryCount++
			time.Sleep(time.Second)
			if retryCount > 30 {
				return fmt.Errorf("unable to connect to target after %d retries", retryCount)
			}
		}
	}
	defer outConn.Close()

	// Write the header to the writer
	_, err = outConn.Write([]byte(identifier))
	if err != nil {
		return err
	}

	go func() {
		n, _ := io.Copy(inConn, outConn)
		b.log.Info("bytes copied from server to client", "count", n)
	}()

	n, err := io.Copy(outConn, inConn)
	if err != nil {
		return err
	}
	b.log.Info("bytes copied", "count", n)
	return nil
}
