package proxy

import (
	"bufio"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// streamResponse copies response body with immediate flushing for each chunk.
func streamResponse(w gin.ResponseWriter, r io.Reader) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fallback: just copy without flushing
		io.Copy(w, r)
		return
	}

	buf := bufio.NewReader(r)
	for {
		line, err := buf.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				// Connection closed or error
			}
			return
		}

		// Write the line
		w.Write(line)

		// Flush immediately for streaming
		flusher.Flush()
	}
}
