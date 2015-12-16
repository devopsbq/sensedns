package main

import (
	"encoding/json"
	"log"
	"path"
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
	containerID, hostname := event.ID, container.Config.Hostname
	log.Printf("Container %s... (%s): %s\n", containerID[0:8], hostname, event.Status)
	var keys []string
	for net, v := range container.NetworkSettings.Networks {
		s.newHostWithNetwork(net)
		key := path.Join(storePath, net, hostname, containerID)
		pair := &api.KVPair{Key: key, Value: []byte(v.IPAddress)}
		if _, err := s.consulKV.Put(pair, nil); err != nil {
			log.Printf("Error operating with key: %s\n", err)
			continue // FIXME: do something about this
		}
		keys = append(keys, key)
	}
	inventoryKey := path.Join(inventoryPath, s.NodeID, containerID)
	keyBytes, _ := json.Marshal(keys)
	inventoryPair := &api.KVPair{Key: inventoryKey, Value: keyBytes}
	if _, err := s.consulKV.Put(inventoryPair, nil); err != nil {
		log.Printf("Error operating with key: %s\n", err)
	}
}

func (s *SenseDNS) deleteContainer(event *docker.APIEvents) {
	containerID := event.ID
	inventoryKey := path.Join(inventoryPath, s.NodeID, containerID)
	hostname, networks, err := s.removeFromConsul(inventoryKey, []string{})
	if err != nil {
		log.Printf("Error operating with key: %s\n", err)
	}
	log.Printf("Container %s... (%s): %s\n", containerID[0:8], hostname, event.Status)
	for _, net := range networks {
		s.removedHostWithNetwork(net)
	}
}

func (s *SenseDNS) removeFromConsul(inventoryKey string, networkKeys []string) (hostname string, nets []string, err error) {
	var pair *api.KVPair
	if len(networkKeys) == 0 {
		pair, _, err = s.consulKV.Get(inventoryKey, nil)
		if err != nil {
			return
		}
		json.Unmarshal(pair.Value, &networkKeys)
	}
	if _, err = s.consulKV.Delete(inventoryKey, nil); err != nil {
		return
	}
	for _, networkKey := range networkKeys {
		_, hostname = path.Split(path.Dir(networkKey))
		_, net := path.Split(path.Dir(path.Dir(networkKey)))
		nets = append(nets, net)
		_, errr := s.consulKV.Delete(networkKey, nil)
		if errr != nil {
			err = errr
		}
	}
	return
}

func (s *SenseDNS) newHostWithNetwork(net string) {
	if _, ok := s.KnownNets[net]; !ok {
		log.Printf("Network <%s>: added\n", net)
		s.KnownNets[net] = 0
		info, _ := s.dockerClient.NetworkInfo(net)
		switch info.Driver {
		case "host", "null", "bridge":
		default:
			go s.addNetwork(net)
		}
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
		index = meta.LastIndex
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
			var networkKeys, networks []string
			json.Unmarshal(value.Value, &networkKeys)
			for _, networkKey := range networkKeys {
				_, net := path.Split(path.Dir(path.Dir(networkKey)))
				networks = append(networks, net)
			}
			hostname, networks, err := s.removeFromConsul(value.Key, networks)
			if err != nil {
				log.Printf("Error operating with key: %s\n", err)
				continue // FIXME: do something about this
			}
			log.Printf("Container %s... (%s): %s\n", id[0:8], hostname, "removed")
		}
	}
	for _, c := range containers {
		s.addContainer(&docker.APIEvents{ID: c.ID, Status: "existing"})
	}
}
