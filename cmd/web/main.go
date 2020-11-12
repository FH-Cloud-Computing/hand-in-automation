package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

var indexPage string = `<!DOCTYPE html>
<html lang="en">
    <head>
        <title>QDOS - Quick and Dirty handin Orchestration System</title>
    </head>
    <body>
        <form method="POST" action="/queue">
            <label>
                Clone URL:
                <input name="url" type="url" placeholder="https://github.com/..." />
            </label>
            <label>
                Clone username:
                <input name="username" type="text" placeholder="youruser" />
            </label>
            <label>
                Clone password or personal access token:
                <input name="password" type="password" placeholder="" />
                <p><strong>Note:</strong> if your Git account uses two-factor authentication make sure to submit a personal access token.</p>
                <p><strong>Note:</strong> Your credentials are only used during the checkout and are only kept in-memory. They are deleted immediately once the initial checkout is complete.</p>
                <p><strong>Note:</strong> GitHub has strict rate limits. If you run into problems without authentication try and provide a personal access token.</p>
            </label>
            <button>Submit</button>
        </form>
    </body>
</html>`

type webserver struct {
	q *queue
}

func (www *webserver) enqueueAction(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(fmt.Sprintf("<h1>Bad request: %v</h1>", err)))
		return
	}

	queueId := www.q.add(req.FormValue("url"), req.FormValue("username"), req.FormValue("password"))

	url := fmt.Sprintf("http://%s/queue/%s", req.Host, queueId)
	w.Header().Set("Location", url)
	w.WriteHeader(302)
}

func (www *webserver) getAction(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	queueEntry := www.q.get(vars["id"])
	w.Header().Set("Content-Type", "text/html")
	if queueEntry == nil {
		w.WriteHeader(404)
		w.Write([]byte("<h1>Queue entry not found</h1>"))
		return
	}

	w.Write([]byte("<!DOCTYPE html>"))
	w.Write([]byte("<html lang=\"en\">"))
	w.Write([]byte("<head><title></title>"))
	if queueEntry.status != "finished" {
		w.Write([]byte("<meta http-equiv=\"refresh\" content=\"5\" />"))
	}
	w.Write([]byte("<style>body { font-family: monospace; } tr { vertical-align: top;} td { padding:0.25rem; } .debug { color: #999999; } .info { color: #00a000; } .notice { color: #0000a0; } .warning { color: #a0a000; } .error { color: #a00000; }</style>"))
	w.Write([]byte("</head>"))
	w.Write([]byte("<body>"))
	w.Write([]byte("<table>"))
	fh, err := os.Open(queueEntry.logfile)
	if err != nil {
		w.Write([]byte("<tr><td>Waiting for log file...</td></tr>"))
	} else {
		defer fh.Close()
		data, err := ioutil.ReadAll(fh)
		if err != nil {
			w.Write([]byte("<tr><td>Waiting for log file...</td></tr>"))
		} else {
			w.Write(data)
		}
	}
	w.Write([]byte("</table>"))
	w.Write([]byte("<script>window.scrollTo(0,document.body.scrollHeight);</script>"))
	w.Write([]byte("</body>"))
	w.Write([]byte("</html>"))
}

func (www *webserver) indexAction(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/html")
	writer.Write([]byte(indexPage))
}

func main() {
	apiKey := os.Getenv("EXOSCALE_KEY")
	apiSecret := os.Getenv("EXOSCALE_SECRET")
	logdir := os.Getenv("LOGDIR")
	projectdir := os.Getenv("PROJECTDIR")

	www := webserver{
		q: newQueue(
			apiKey,
			apiSecret,
			logdir,
			projectdir,
		),
	}
	go www.q.run()

	r := mux.NewRouter()
	r.HandleFunc("/queue/{id}", www.getAction)
	r.HandleFunc("/queue", www.enqueueAction)
	r.HandleFunc("/", www.indexAction)
	http.ListenAndServe(":8090", r)
}
