package main

import (
	"log"
	"path"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/hashicorp/consul/api"
)

type SenseDNS struct {
	NodeID       string
	KnownNets    map[string]int
	dockerClient *docker.Client
	consulKV     *api.KV
	dnsServer    *Server
}

func (s *SenseDNS) addContainer(event *docker.APIEvents) {
	container, _ := s.dockerClient.InspectContainer(event.ID)
	log.Printf("Container %s... (%s): %s\n", event.ID[0:8], container.Config.Hostname, event.Status)
	for k, v := range container.NetworkSettings.Networks {
		key := path.Join(storePath, k, container.Config.Hostname, container.ID)
		pair := &api.KVPair{Key: key, Value: []byte(v.IPAddress)}
		if _, err := s.consulKV.Put(pair, nil); err != nil {
			log.Printf("Error operating with key: %s\n", err)
			continue // FIXME: do something about this
		}
		inventoryKey := path.Join(inventoryPath, s.NodeID, container.ID)
		inventoryPair := &api.KVPair{Key: inventoryKey, Value: []byte(key)}
		if _, err := s.consulKV.Put(inventoryPair, nil); err != nil {
			log.Printf("Error operating with key: %s\n", err)
			continue // FIXME: do something about this
		}
		s.newHostWithNetwork(k)
	}
}

func (s *SenseDNS) deleteContainer(event *docker.APIEvents) {
	container, _ := s.dockerClient.InspectContainer(event.ID)
	log.Printf("Container %s... (%s): %s\n", event.ID[0:8], container.Config.Hostname, event.Status)
	for k := range container.NetworkSettings.Networks {
		key := path.Join(storePath, k, container.Config.Hostname, container.ID)
		inventoryKey := path.Join(inventoryPath, s.NodeID, container.ID)
		if err := s.removeFromConsul(key, inventoryKey); err != nil {
			log.Printf("Error operating with key: %s\n", err)
			continue // FIXME: do something about this
		}
		s.removedHostWithNetwork(k)
	}
}

func (s *SenseDNS) removeFromConsul(key, inventoryKey string) error {
	if _, err := s.consulKV.Delete(key, nil); err != nil {
		return err
	}
	if _, err := s.consulKV.Delete(inventoryKey, nil); err != nil {
		return err
	}
	return nil
}

func (s *SenseDNS) newHostWithNetwork(net string) {
	if _, ok := s.KnownNets[net]; !ok {
		log.Printf("Network <%s>: added\n", net)
		s.KnownNets[net] = 0
		go s.addNetwork(net)
	}
	s.KnownNets[net]++
}

func (s *SenseDNS) removedHostWithNetwork(net string) {
	s.KnownNets[net]--
	if v := s.KnownNets[net]; v == 0 {
		delete(s.KnownNets, net)
		log.Printf("Network <%s>: forgot\n", net)
	}
}

func (s *SenseDNS) addNetwork(net string) {
	log.Printf("Network <%s>: start watching\n", net)
	requestDuration := consulTimeout
	index := uint64(0)
	for {
		queryOptions := &api.QueryOptions{
			AllowStale: true,
			WaitIndex:  index,
			WaitTime:   requestDuration,
		}
		pairs, meta, err := s.consulKV.List(path.Join(storePath, net), queryOptions)
		if err != nil {
			log.Printf("Network <%s>: error while watching\n", net)
			time.Sleep(time.Second)
			continue
		}
		if meta.RequestTime > requestDuration {
			log.Printf("Network <%s>: step watching\n", net)
			continue
		}
		log.Printf("Network <%s>: updated\n", net)
		s.fillWithData(pairs, net)
		if _, ok := s.KnownNets[net]; !ok {
			log.Printf("Network <%s>: stop watching\n", net)
			return
		}
		index = meta.LastIndex + 1
	}
}

func (s *SenseDNS) boot() {
	containers, err := s.dockerClient.ListContainers(docker.ListContainersOptions{All: false})
	if err != nil {
		log.Fatalf("Error loading containers at boot; %v\n", err)
	}
	pairs, _, err := s.consulKV.List(path.Join(inventoryPath, s.NodeID), nil)
	if err != nil {
		log.Fatalf("Error loading old info at boot: %v\n", err)
	}
	for _, value := range pairs {
		_, id := path.Split(value.Key)
		found := false
		for _, c := range containers {
			if found = c.ID == id; found {
				break
			}
		}
		if !found {
			dirSplit := strings.Split(string(value.Value), "/")
			log.Printf("Container %s... (%s): %s\n", id[0:8], dirSplit[3], "removed")
			if err = s.removeFromConsul(string(value.Value), value.Key); err != nil {
				log.Printf("Error operating with key: %s\n", err)
				continue // FIXME: do something about this
			}
		}
	}
	for _, c := range containers {
		s.addContainer(&docker.APIEvents{ID: c.ID, Status: "existing"})
	}
}
