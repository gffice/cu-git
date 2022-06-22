// Client implementation of the Conjure PT adapted for Tor
package main

import (
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/refraction-networking/gotapdance/tapdance"

	pt "git.torproject.org/pluggable-transports/goptlib.git"
	"git.torproject.org/pluggable-transports/snowflake.git/common/safelog"
)

type ConjureConfig struct {
	assetDir    string
	registerURL string //URL of the conjure bidirectional registration API endpoint
	front       string
}

// Get SOCKS arguments and populate config
func getSOCKSArgs(conn net.Conn, config *ConjureConfig) {
	return

}

// handle the SOCKS conn
func handler(conn net.Conn, config *ConjureConfig) error {
	return handle(conn, config)
}

//TODO: pass in shutdown channel?
func acceptLoop(ln *pt.SocksListener, config *ConjureConfig) error {
	defer ln.Close()

	for {
		conn, err := ln.AcceptSocks()
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Temporary() {
				pt.Log(pt.LogSeverityError, "accept error: "+err.Error())
				continue
			}
			return err
		}
		log.Printf("SOCKS accepted: %v", conn.Req)
		getSOCKSArgs(conn, config)
		go func() {
			err := handler(conn, config)
			if err != nil {
				log.Println(err)
			}
		}()
	}
	return nil
}

func main() {
	assetDir := flag.String("assets", "", "assetDir")
	logFilename := flag.String("log", "", "name of the log file")
	logToStateDir := flag.Bool("log-to-state-dir", false,
		"resolve the log file relative to tor's pt state dir")
	unsafeLogging := flag.Bool("unsafe-logging", false, "prevent logs from being scrubbed")
	front := flag.String("front", "", "domain front")
	registerURL := flag.String("registerURL", "", "URL of the conjure registration station")

	flag.Parse()

	// Set up logging
	var logFile io.Writer
	logFile = ioutil.Discard
	if *logFilename != "" {
		if *logToStateDir {
			stateDir, err := pt.MakeStateDir()
			if err != nil {
				log.Fatal(err)
			}
			*logFilename = filepath.Join(stateDir, *logFilename)
		}
		f, err := os.OpenFile(*logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		logFile = f
	}
	if !*unsafeLogging {
		logFile = &safelog.LogScrubber{Output: logFile}
	}
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetOutput(logFile)

	tapdance.SetLoggerOutput(logFile)
	tapdance.Logger().Warnf("Redirecting log to file")

	// Configure Conjure
	config := &ConjureConfig{
		assetDir:    *assetDir,
		registerURL: *registerURL,
		front:       *front,
	}

	// Tor client-side transport setup
	var ln *pt.SocksListener
	ptInfo, err := pt.ClientSetup(nil)
	if err != nil {
		log.Fatal(err)
	}
	if ptInfo.ProxyURL != nil {
		pt.ProxyError("proxy is not supported")
		os.Exit(1)
	}

	for _, methodName := range ptInfo.MethodNames {
		switch methodName {
		case "conjure":
			ln, err = pt.ListenSocks("tcp", "127.0.0.1:0")
			if err != nil {
				pt.CmethodError(methodName, err.Error())
				break
			}
			log.Printf("Started SOCKS listener at %v", ln.Addr())
			go acceptLoop(ln, config)
			pt.Cmethod(methodName, ln.Version(), ln.Addr())
		default:
			pt.CmethodError(methodName, "no such method")
		}
	}
	pt.CmethodsDone()

	// shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)

	// https://gitweb.torproject.org/torspec.git/tree/pt-spec.txt#n203
	if os.Getenv("TOR_PT_EXIT_ON_STDIN_CLOSE") == "1" {
		go func() {
			if _, err := io.Copy(ioutil.Discard, os.Stdin); err != nil {
				log.Printf("Error copying os.Stdin to ioutil.Discard: %v", err)
			}
			log.Printf("Terminating because of stdin close")
			sigChan <- syscall.SIGTERM
		}()
	}

	<-sigChan
	log.Println("shutting down conjure")
	ln.Close()
}
