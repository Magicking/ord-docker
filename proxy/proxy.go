package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"crypto/tls"
)

func main() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	// Get environment variables
	listenAddr := os.Getenv("LISTEN_ADDR")
	listenPort := os.Getenv("LISTEN_PORT")
	remoteAddr := os.Getenv("REMOTE_ADDR")
	remotePort := os.Getenv("REMOTE_PORT")

	// Listen on the specified address and port
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%s", listenAddr, listenPort))
	if err != nil {
		log.Fatalf("Error starting listener: %s", err.Error())
	}
	defer listener.Close()

	log.Printf("Listening on %s:%s", listenAddr, listenPort)

	// Start accepting connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %s", err.Error())
			continue
		}

		// Start a goroutine to handle the connection
		go handleConnection(conn, remoteAddr, remotePort)
	}
}

func processInputJsonRPC(input []byte) ([]byte, error) {
	var request map[string]interface{}
	input = []byte(strings.TrimSpace(string(input)))
	err := json.Unmarshal(input, &request)
	if err != nil {
		return nil, err
	}

	switch request["method"] {
	case "getrawtransaction":
		params, ok := request["params"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("Invalid input: params is missing or not an array")
		}

		for i, param := range params {
			boolVal, ok := param.(bool)
			if ok {
				var bVal int
				if boolVal {
					bVal = 1
				}
				params[i] = bVal
			}
		}
	}

	output, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func processOutputJsonRPC(input []byte) (output []byte, internalError bool, err error) {
	var request map[string]interface{}
	err = json.Unmarshal(input, &request)
	if err != nil {
		return nil, internalError, err
	}

	if request["error"] != nil {

		internalError = true

		/*
		TODO: #2
		msg, ok := request["error"]["message"]
		if !ok {
			// Keep original error and print it
			log.Printf("Error from remote server: %+v", request["error"])
		} else {
			if strings.Contains(msg, "Block not available (pruned data)") {
				request["error"] = map[string]interface{}{
					"code":    -32603,
					"message": "Internal error",
				}
			}
		}
		*/
	}

	output, err = json.Marshal(request)
	if err != nil {
		return nil, internalError, err
	}

	return output, internalError, nil
}

func makeRequest(headers map[string]string, remoteHost string, payload []byte) (*http.Response, []byte, error) {
	req, err := http.NewRequest("POST", remoteHost, bytes.NewBuffer(payload))
	if err != nil {
		return nil, nil, err
	}

	// Set headers on the request
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	return resp, respBody, nil
}

func handleConnection(conn net.Conn, remoteAddr string, remotePort string) {
	defer conn.Close()

	remotehost := fmt.Sprintf("https://%s:%s", remoteAddr, remotePort)

	// Create a buffered reader for reading from the connection
	reader := bufio.NewReader(conn)
	var headers map[string]string

	// Read lines from the connection and forward them to the remote server
	for {
		postKo := true
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					// End of file, connection was closed by the client
					log.Printf("Connection closed by client")
				} else {
					// Other error, log and close the connection
					log.Printf("Error reading from connection: %s", err.Error())
				}
				goto ERRORINCOMING
			}
			// Waiting for this to reset the connection
			if len(line) > 4 && string(line[0:4]) == "POST" {
				postKo = false
				headers = map[string]string{"Host": fmt.Sprint("%s:%s", remoteAddr, remotePort)}
				continue
			}
			if postKo {
				time.Sleep(1 * time.Second)
				continue
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			header := strings.SplitN(line, ":", 2)
			if len(header) != 2 {
				goto ERRORINCOMING
			}
			headers[strings.ToLower(strings.TrimSpace(header[0]))] = strings.TrimSpace(header[1])
		}
		/* Check that the header has:
		   - Content-Length
		*/
		contentLength, ok := headers["content-length"]
		if !ok {
			goto ERRORINCOMING
		}
		payloadLength, err := strconv.Atoi(strings.TrimSpace(contentLength))
		if err != nil {
			log.Println(err)
			goto ERRORINCOMING
		}
		// Read the payload until payloadLength
		payload, err := reader.Peek(payloadLength)
		if err != nil {
			log.Println(err)
			log.Println(payload)
			goto ERRORINCOMING
		}
		if len(payload) != payloadLength {
			goto ERRORINCOMING
		}
		n, err := reader.Discard(payloadLength)
		if err != nil {
			log.Println(err)
			goto ERRORINCOMING
		}

		if n != payloadLength {
			goto ERRORINCOMING
		}
		// TODO Process payload and modify or dispatch if needed for wallet operations
		log.Println(string(payload))
		payload, err = processInputJsonRPC(payload)
		if err != nil {
			log.Println(err)
			goto ERRORINCOMING
		}
		// Send the request to the remote server
		resp, respBody, err := makeRequest(headers, remotehost, payload)
		if err != nil {
			log.Println(err)
			goto ERRORINCOMING
		}
		log.Println(string(respBody))

		statusCode := resp.StatusCode
		output, ok, err := processOutputJsonRPC(respBody)
		if err != nil {
			log.Println(err)
			goto ERRORINCOMING
		}
		if ok {
			statusCode = 500
		}
		helloHTTP := fmt.Sprintf("HTTP/1.1 %03d OK\r\n", statusCode)
		// Write the response to the client
		buf := bytes.NewBuffer([]byte(helloHTTP))
		resp.Header.Set("content-length", strconv.Itoa(len(output)))
		for key, value := range resp.Header {
			_, err = fmt.Fprintf(buf, "%s: %s\r\n", strings.ToLower(key), value[0])
			if err != nil {
				log.Println(err)
				goto ERRORINCOMING
			}
		}
		buf.WriteByte('\r')
		buf.WriteByte('\n')
		buf.Write(output)
		_, err = conn.Write(buf.Bytes())
		if err != nil {
			log.Println(err)
			goto ERRORINCOMING
		}
		if statusCode != 200 {
			goto ERRORINCOMING
		}
	}
ERRORINCOMING:
	time.Sleep(1 * time.Second)
	log.Printf("Connection closed")
}
