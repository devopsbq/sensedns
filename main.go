package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
	"github.com/hashicorp/consul/api"
)

const (
	storePath     = "sensedns/network"
	inventoryPath = "sensedns/inventory"
	version       = "v0.1.3"
)

var log = logrus.New()

func getOrElse(env, def string) string {
	value := os.Getenv(env)
	if value == "" {
		return def
	}
	return value
}

func getNodeID(client *docker.Client) string {
	info, err := client.Info()
	if err != nil {
		log.Fatalln("Can't get node ID")
	}
	return fmt.Sprintf("%s-%s", info.Get("ClusterAdvertise"), info.Get("Name"))
}

func main() {
	log.Printf("Starting SenseDNS %s", version)
	dnsServer := newDNS()

	var timeout string
	var consulURL string
	var logLevel string

	flag.StringVar(&consulURL, "c", getOrElse("CONSUL_URL", "127.0.0.1:8500"), "The consul URL with format IP:PORT.")
	flag.StringVar(&logLevel, "l", getOrElse("LOG_LEVEL", "info"), "The level of logging")
	flag.StringVar(&timeout, "t", getOrElse("CONSUL_TIMEOUT", "5m"), "The URL of zones in JSON format.")
	flag.StringVar(&dnsServer.host, "a", getOrElse("DNS_LISTEN_ADDRESS", "0.0.0.0"), "The IP of the DNS server.")
	flag.StringVar(&dnsServer.port, "p", getOrElse("DNS_LISTEN_PORT", "53"), "The port of the DNS server.")
	flag.StringVar(&dnsServer.recurseTo, "r", getOrElse("REDIRECT_DNS", "8.8.8.8:53"), "DNS to ask if request fails.")
	flag.StringVar(&dnsServer.networkTLD, "n", getOrElse("NETWORK_TLD", "sensedns"), "The networks TLD to use.")
	flag.Parse()

	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("Log level not valid: %s\n", err)
	}
	log.Level = level

	consulTimeout, err := time.ParseDuration(timeout)
	if err != nil {
		log.Fatalf("Timeout provided not valid: %s\n", err)
	}
	dockerClient, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		log.Fatalf("Can't connect with docker socket %s\n", err)
	}
	events := make(chan *docker.APIEvents)
	if err = dockerClient.AddEventListener(events); err != nil {
		log.Fatalf("Can't add event listener to docker %s\n", err)
	}
	consulClient, err := api.NewClient(&api.Config{Address: consulURL, WaitTime: consulTimeout})
	if err != nil {
		log.Fatalf("Can't connect to consul: %s\n", err)
	}

	sense := &SenseDNS{
		NodeID:        getNodeID(dockerClient),
		KnownNets:     make(map[string]int),
		HostCache:     make(map[string]string),
		dockerClient:  dockerClient,
		consulKV:      consulClient.KV(),
		consulTimeout: consulTimeout,
		dnsServer:     dnsServer,
	}
	go sense.dnsServer.Run()

	sense.boot()
	for e := range events {
		switch e.Status {
		case "start", "unpause":
			sense.addContainer(e)
		case "die", "pause":
			sense.deleteContainer(e, true)
		}
	}
}
