package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/hashicorp/consul/api"
)

const (
	storePath     = "sensedns/network"
	inventoryPath = "sensedns/inventory"
	version       = "v0.1.1"
)

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
	log.Printf("Starting SenseDNS %s\n", version)
	dnsServer := &Server{
		rTimeout: 5 * time.Second,
		wTimeout: 5 * time.Second,
		zones: &ZoneStore{
			store: make(map[string]Zone),
		},
	}

	flag.StringVar(&consulURL, "c", getOrElse("CONSUL_URL", "127.0.0.1:8500"), "The consul URL with format IP:PORT.")
	flag.StringVar(&timeout, "t", getOrElse("CONSUL_TIMEOUT", "5m"), "The URL of zones in JSON format.")
	flag.StringVar(&networkTLD, "n", getOrElse("NETWORK_TLD", "sensedns"), "The networks TLD to use.")
	flag.StringVar(&dnsServer.host, "l", getOrElse("DNS_LISTEN_ADDRESS", "0.0.0.0"), "The IP of the DNS server.")
	flag.StringVar(&dnsServer.port, "p", getOrElse("DNS_LISTEN_PORT", "53"), "The port of the DNS server.")
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
		NodeID:       getNodeID(dockerClient),
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
