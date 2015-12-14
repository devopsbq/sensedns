package main

import (
	"log"
	"path"
	"sync"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/hashicorp/consul/api"
)

type SenseDNS struct {
	StorePath    string
	KnownNets    map[string]int
	dockerClient *docker.Client
	consulKV     *api.KV
	dnsServer    *Server
	sync.Mutex
}

func (s *SenseDNS) addContainer(event *docker.APIEvents) {
	container, _ := s.dockerClient.InspectContainer(event.ID)
	log.Printf("Container %s... (%s): %s\n", event.ID[0:8], container.Config.Hostname, event.Status)
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
		go s.addNetwork(net)
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

func (s *SenseDNS) addNetwork(net string) {
	log.Printf("Start watching: %s\n", net)
	requestDuration := consulTimeout
	index := uint64(0)
	for {
		queryOptions := &api.QueryOptions{
			AllowStale: true,
			WaitIndex:  index,
			WaitTime:   requestDuration,
		}
		pairs, meta, err := s.consulKV.List(path.Join(s.StorePath, net), queryOptions)
		if err != nil {
			log.Printf("Error while watching: %s\n", net)
			time.Sleep(time.Second)
			continue
		}
		if meta.RequestTime > requestDuration {
			log.Printf("Step watching: %s\n", net)
			continue
		}
		log.Printf("Network %s: update\n", net)
		s.fillWithData(pairs, net)
		if _, ok := s.KnownNets[net]; !ok {
			log.Printf("Stop watching: %s\n", net)
			return
		}
		s.Lock()
		index = meta.LastIndex + 1
		s.Unlock()
	}
}

func (s *SenseDNS) boot() {
	containers, err := s.dockerClient.ListContainers(docker.ListContainersOptions{All: false})
	if err != nil {
		log.Fatalln("Error loading containers at boot.")
	}
	for _, c := range containers {
		s.addContainer(&docker.APIEvents{ID: c.ID, Status: "existing"})
	}
}
