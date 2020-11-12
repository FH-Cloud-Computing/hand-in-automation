package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path"
	"runtime"
	"sync"

	"github.com/containerssh/log"
	"github.com/containerssh/log/pipeline"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/exoscale/egoscale"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/janoszen/exoscale-account-wiper/plugin"

	handin "github.com/FH-Cloud-Computing/hand-in-automation"
	log2 "github.com/FH-Cloud-Computing/hand-in-automation/logger"
)

func newQueue(apiKey string, apiSecret string, logdir string, projectdir string) *queue {
	ctx := context.Background()
	logger := pipeline.NewLoggerPipelineFactory(&log2.LogFormatter{}, os.Stdout).Make(log.LevelNotice)

	if apiKey == "" || apiSecret == "" {
		logger.Errorf("EXOSCALE_KEY and EXOSCALE_SECRET must be provided")
		defer os.Exit(1)
		runtime.Goexit()
	}

	clientFactory := plugin.NewClientFactory(apiKey, apiSecret)

	logger.Infof("running preflight checks...")

	logger.Debugf("checking Docker socket...")
	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Infof("failed to create Docker client (%v)", err)
		defer os.Exit(1)
		runtime.Goexit()
	}
	dockerClient.NegotiateAPIVersion(ctx)

	if _, err := dockerClient.Ping(ctx); err != nil {
		logger.Errorf("error: could not ping Docker socket (%v)\n", err)
		os.Exit(1)
	}
	logger.Debugf("check!")

	logger.Debugf("pulling Terraform execution image...")
	pullResult, err := dockerClient.ImagePull(ctx, "docker.io/janoszen/terraform", types.ImagePullOptions{})
	if err != nil {
		logger.Errorf("error: could not pull Terraform image (%v)\n", err)
		os.Exit(1)
	}
	if _, err := handin.DockerToLogger(pullResult, logger); err != nil {
		logger.Errorf("error: could not pull Terraform image (%v)\n", err)
		os.Exit(1)
	}
	logger.Debugf("check!")

	logger.Debugf("checking Exoscale credentials...")
	if _, err := clientFactory.GetExoscaleClient().RequestWithContext(ctx, egoscale.ListZones{}); err != nil {
		logger.Errorf("error: could not list Exoscale zones (%v)\n", err)
		os.Exit(1)
	}
	logger.Debugf("check!")
	logger.Debugf("preflight checks complete, ready for takeoff.")

	return &queue{
		entriesById:   map[string]*queueEntry{},
		queue:         []*queueEntry{},
		lock:          &sync.Mutex{},
		available:     make(chan bool, 1),
		logdir:        logdir,
		projectdir:    projectdir,
		logger:        logger,
		clientFactory: clientFactory,
		dockerClient:  dockerClient,
	}
}

type queue struct {
	entriesById   map[string]*queueEntry
	queue         []*queueEntry
	available     chan bool
	lock          *sync.Mutex
	logdir        string
	projectdir    string
	logger        log.Logger
	clientFactory *plugin.ClientFactory
	dockerClient  *client.Client
}

type queueEntry struct {
	id        string
	url       string
	username  string
	password  string
	status    string
	directory string
	logfile   string
}

func (q *queue) add(gitUrl string, username string, password string) string {
	u := make([]byte, 16)
	_, err := rand.Read(u)
	if err != nil {
		panic(err)
	}
	entryId := base64.URLEncoding.EncodeToString(u)

	entry := &queueEntry{
		id:        entryId,
		url:       gitUrl,
		username:  username,
		password:  password,
		status:    "queued",
		directory: path.Join(q.projectdir, entryId),
		logfile:   path.Join(q.logdir, entryId),
	}
	q.lock.Lock()
	q.entriesById[entryId] = entry
	q.queue = append(q.queue, entry)

	q.lock.Unlock()

	select {
	case q.available <- true:
	default:
	}

	return entryId
}

func (q *queue) get(id string) *queueEntry {
	if entry, ok := q.entriesById[id]; ok {
		return entry
	}
	return nil
}

func (q *queue) run() {
	for {
		<-q.available
		item := q.queue[0]

		logFile, err := os.Create(item.logfile)
		if err != nil {
			panic(err)
		}

		logger := pipeline.NewLoggerPipelineFactory(&HTMLLogFormatter{}, logFile).Make(log.LevelDebug)

		q.runEntry(item, logger)

		logger.Infof("tests finished, wiping data directory...")
		if err := os.RemoveAll(item.directory); err != nil {
			logger.Warningf("failed to remove project directory %s (%v)", item.directory, err)
		}
		item.status = "finished"

		q.lock.Lock()
		q.queue = q.queue[1:]
		q.lock.Unlock()
	}
}

func (q *queue) runEntry(item *queueEntry, logger log.Logger) {
	item.status = "running"

	if err := os.Mkdir(item.directory, os.ModePerm); err != nil {
		logger.Errorf("failed to create project directory (%v)", err)
		return
	}

	var auth transport.AuthMethod
	if item.username != "" && item.password != "" {
		auth = &http.BasicAuth{
			Username: item.username,
			Password: item.password,
		}
	}

	logger.Infof("cloning git repository at %s", item.url)
	if _, err := git.PlainClone(item.directory, false, &git.CloneOptions{
		URL:          item.url,
		Auth:         auth,
		SingleBranch: true,
	}); err != nil {
		logger.Errorf("failed to clone repository (%v)", err)
		return
	}
	logger.Infof("git clone successful, dropping git credentials.")

	item.username = ""
	item.password = ""

	logger.Infof("running tests...")
	if err := handin.RunTests(
		context.Background(),
		q.clientFactory,
		q.dockerClient,
		item.directory,
		logger,
	); err != nil {
		logger.Errorf("failed to run tests (%v)", err)
	}
}
