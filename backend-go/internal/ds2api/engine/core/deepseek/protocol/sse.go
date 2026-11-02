package protocol

import (
	"bufio"
	"io"
	"net/http"
)

func ScanSSELines(resp *http.Response, onLine func([]byte) bool) error {
	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if !onLine(line) {
				return nil
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
