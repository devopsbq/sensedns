package main

import (
	"log"
	"path"

	"github.com/fsouza/go-dockerclient"
	"github.com/hashicorp/consul/api"
)

type SenseDNS struct {
	StorePath    string
	KnownNets    map[string]int
	dockerClient *docker.Client
	consulKV     *api.KV
}

// TODO: on start see what containers there are and store them on consul
// TODO: provite WriteOptions with token if needed

func main() {
	dockerClient, _ := docker.NewClient("unix:///var/run/docker.sock")
	events := make(chan *docker.APIEvents)
	if err := dockerClient.AddEventListener(events); err != nil {
		log.Fatalf("Error: %s\n", err)
	}

	consulClient, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		log.Fatalf("Error: %s\n", err)
	}

	sense := &SenseDNS{
		StorePath:    "sensedns/network",
		KnownNets:    make(map[string]int),
		dockerClient: dockerClient,
		consulKV:     consulClient.KV(),
	}

	for e := range events {
		switch e.Status {
		case "start", "unpause":
			sense.addContainer(e)
		case "die", "pause":
			sense.deleteContainer(e)
		}
	}
}

func (s *SenseDNS) addContainer(event *docker.APIEvents) {
	log.Printf("Container %s: %s\n", event.ID, event.Status)
	container, _ := s.dockerClient.InspectContainer(event.ID)
	for k, v := range container.NetworkSettings.Networks {
		key := path.Join(s.StorePath, k, container.Config.Hostname, container.ID)
		p := &api.KVPair{Key: key, Value: []byte(v.IPAddress)}
		if _, err := s.consulKV.Put(p, nil); err != nil {
			log.Printf("Error operating with key: %s\n", err)
			continue // TODO: do something about this
		}
		s.newHostWithNetwork(k)
	}
}

func (s *SenseDNS) deleteContainer(event *docker.APIEvents) {
	log.Printf("Container %s: %s\n", event.ID, event.Status)
	container, _ := s.dockerClient.InspectContainer(event.ID)
	for k := range container.NetworkSettings.Networks {
		key := path.Join(s.StorePath, k, container.Config.Hostname, container.ID)
		if _, err := s.consulKV.Delete(key, nil); err != nil {
			log.Printf("Error operating with key: %s\n", err)
			continue // TODO: do something about this
		}
		s.removedHostWithNetwork(k)
	}
}

func (s *SenseDNS) newHostWithNetwork(net string) {
	if _, ok := s.KnownNets[net]; !ok {
		log.Printf("Network %s: new\n", net)
		s.KnownNets[net] = 0
	}
	s.KnownNets[net]++
}

func (s *SenseDNS) removedHostWithNetwork(net string) {
	s.KnownNets[net]--
	if v := s.KnownNets[net]; v == 0 {
		delete(s.KnownNets, net)
		log.Printf("Network %s: forgot\n", net)
	}
}
