package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/hashicorp/consul/api"
)

func getOrElse(env, def string) string {
	value := os.Getenv(env)
	if value == "" {
		return def
	}
	return value
}

func main() {
	dnsServer := &Server{
		port:     53,
		rTimeout: 5 * time.Second,
		wTimeout: 5 * time.Second,
		zones: &ZoneStore{
			store: make(map[string]Zone),
		},
	}

	flag.StringVar(&consulURL, "c", getOrElse("CONSUL_URL", "127.0.0.1:8500"), "The consul URL with format IP:PORT.")
	flag.StringVar(&timeout, "t", getOrElse("CONSUL_TIMEOUT", "5m"), "The URL of zones in JSON format.")
	flag.StringVar(&dnsServer.host, "l", getOrElse("DNS_LISTEN_ADDRESS", "0.0.0.0"), "The IP of the DNS server.")
	flag.StringVar(&dnsServer.recurseTo, "r", getOrElse("REDIRECT_DNS", "8.8.8.8:53"), "DNS to ask if request fails.")
	flag.Parse()
	consulTimeout, err = time.ParseDuration(timeout)
	if err != nil {
		log.Fatalf("Timeout provided not valid: %s\n", err)
	}

	dockerClient, _ := docker.NewClient("unix:///var/run/docker.sock")
	events := make(chan *docker.APIEvents)
	if err = dockerClient.AddEventListener(events); err != nil {
		log.Fatalf("Error: %s\n", err)
	}

	consulClient, err := api.NewClient(&api.Config{Address: consulURL})
	if err != nil {
		log.Fatalf("Error: %s\n", err)
	}

	sense := &SenseDNS{
		StorePath:    "sensedns/network",
		KnownNets:    make(map[string]int),
		dockerClient: dockerClient,
		consulKV:     consulClient.KV(),
		dnsServer:    dnsServer,
	}
	go sense.dnsServer.Run()

	sense.boot()
	for e := range events {
		switch e.Status {
		case "start", "unpause":
			sense.addContainer(e)
		case "die", "pause":
			sense.deleteContainer(e)
		}
	}
}
