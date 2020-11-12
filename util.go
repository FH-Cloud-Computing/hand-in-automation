package handin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/containerssh/log"
)

func DockerToLogger(input io.ReadCloser, logger log.Logger) (imageId *string, err error) {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		decoder := json.NewDecoder(strings.NewReader(line))
		data := make(map[string]interface{})
		if err := decoder.Decode(&data); err != nil {
			return nil, fmt.Errorf("failed to decode JSON line: %s (%v)", line, err)
		}
		if val, ok := data["stream"]; ok {
			logger.Debugf("docker:\t%s", strings.TrimSpace(val.(string)))
		} else if val, ok := data["aux"]; ok {
			aux := val.(map[string]interface{})
			if val2, ok2 := aux["ID"]; ok2 {
				id := val2.(string)
				imageId = &id
			} else {
				return nil, fmt.Errorf("invalid response type from Docker daemon: %s", line)
			}
		} else if val, ok := data["status"]; ok {
			status := val.(string)
			id := ""
			if data["id"] != nil {
				id = data["id"].(string)
			}
			logger.Debugf("docker:\t%s:%s", status, id)
		} else {
			return nil, fmt.Errorf("invalid response type from Docker daemon: %s", line)
		}
	}
	return imageId, nil
}

func ReadToLogger(input io.ReadCloser, logger log.Logger) error {
	b := make([]byte, 1)
	var buf bytes.Buffer
	for {
		n, err := input.Read(b)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if n == 0 {
			break
		}
		if bytes.Equal(b, []byte("\n")) {
			line := strings.TrimSpace(buf.String())
			logger.Debugf("terraform:\t%s", line)
			buf = bytes.Buffer{}
		} else {
			buf.Write(b)
		}
	}
	if buf.Len() > 0 {
		line := strings.TrimSpace(buf.String())
		logger.Debugf("terraform:\t%s", line)
	}
	return nil
}
