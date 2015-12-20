package main

import (
	"encoding/json"
	"path"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
	"github.com/hashicorp/consul/api"
)

const (
	networkField   = "network"
	countField     = "count"
	hostnameField  = "hostname"
	containerField = "container"
	indexField     = "index"
)

type cache struct {
	hosts map[string]string
	tam   int
}

// SenseDNS is the proccess which manages containers
type SenseDNS struct {
	NodeID        string
	KnownNets     map[string]int
	HostCache     map[string]string
	dockerClient  *docker.Client
	consulKV      *api.KV
	consulTimeout time.Duration
	dnsServer     *Server
}

func (s *SenseDNS) addContainer(event *docker.APIEvents) {
	container, _ := s.dockerClient.InspectContainer(event.ID)
	containerID, hostname := event.ID, container.Config.Hostname
	s.HostCache[containerID] = hostname
	containerLogger := log.WithFields(logrus.Fields{containerField: containerID[0:8], hostnameField: hostname})
	containerLogger.Info(event.Status)
	var keys []string
	for net, v := range container.NetworkSettings.Networks {
		s.newHostWithNetwork(net) // Que haya fallado lo de 1)) inplica que el hostname o la net no se han recuperado bien
		key := path.Join(storePath, net, hostname, containerID)
		pair := &api.KVPair{Key: key, Value: []byte(v.IPAddress)}
		containerLogger.WithField(networkField, net).Debugf("Inserting network key: %s -> %s", key, v.IPAddress)
		if _, err := s.consulKV.Put(pair, nil); err != nil {
			containerLogger.WithField(networkField, net).Warnf("Error inserting network key on consul: %s", err)
			continue
		}
		keys = append(keys, key)
	}
	inventoryKey := path.Join(inventoryPath, s.NodeID, containerID)
	keyBytes, _ := json.Marshal(keys)
	inventoryPair := &api.KVPair{Key: inventoryKey, Value: keyBytes}
	containerLogger.Debugf("Inserting inventory key: %s -> %s", inventoryKey, string(keyBytes))
	if _, err := s.consulKV.Put(inventoryPair, nil); err != nil {
		containerLogger.Warnf("Error inserting inventory key on consul: %s", err)
	}
}

func (s *SenseDNS) deleteContainer(event *docker.APIEvents, fromSocket bool) {
	containerID := event.ID
	containerLogger := log.WithFields(logrus.Fields{containerField: containerID[0:8], hostnameField: s.HostCache[containerID]})
	containerLogger.Info(event.Status)
	inventoryKey := path.Join(inventoryPath, s.NodeID, containerID)
	pair, _, err := s.consulKV.Get(inventoryKey, nil)
	containerLogger.Debugf("Getting inventory key: %s -> %v", inventoryKey, pair)
	if err != nil {
		log.Warnf("Error deleting inventory key from consul: %s", err)
		return
	}
	var networkKeys []string
	json.Unmarshal(pair.Value, &networkKeys) // TODO: this "panicked" on some situation!!! (pair = niL!)  1))
	if _, err := s.consulKV.Delete(inventoryKey, nil); err != nil {
		log.Warnf("Error deleting inventory key on consul: %s", err)
	}
	for _, networkKey := range networkKeys {
		if _, err := s.consulKV.Delete(networkKey, nil); err != nil {
			log.Warnf("Error deleting network key on consul: %s", err)
			continue
		}
		if fromSocket {
			net := path.Base(path.Dir(path.Dir(networkKey)))
			s.removedHostWithNetwork(net) // TODO: could avoid the problem changing this order
		}
	}
}

func (s *SenseDNS) newHostWithNetwork(net string) {
	networkLogger := log.WithField(networkField, net)
	networkLogger.Debug("new host")
	if _, ok := s.KnownNets[net]; !ok {
		networkLogger.Info("first local host, added network")
		s.KnownNets[net] = 0
		info, _ := s.dockerClient.NetworkInfo(net)
		switch info.Driver {
		case "host", "null", "bridge":
		default:
			go s.addNetwork(net)
		}
	}
	s.KnownNets[net]++
	networkLogger.WithField(countField, s.KnownNets[net]).Debug("number of local hosts changed")
}

func (s *SenseDNS) removedHostWithNetwork(net string) {
	networkLogger := log.WithField(networkField, net)
	networkLogger.Debug("removed host")
	if _, ok := s.KnownNets[net]; ok {
		s.KnownNets[net]--
		networkLogger.WithField(countField, s.KnownNets[net]).Debug("number of local hosts changed")
		if v := s.KnownNets[net]; v == 0 {
			delete(s.KnownNets, net)
			networkLogger.Info("no local hosts, forgot network")
		}
	}
}

func (s *SenseDNS) addNetwork(net string) {
	//TODO: FIX!! IMPORTANTE!!!
	//Es posible que haya más de uno haciendo watch a la misma red si:
	// Se levanta un contenedor en "prueba"
	// Se para el contenedor de "prueba" (el watch sigue activo)!!!
	networkLogger := log.WithField(networkField, net)
	networkLogger.Info("start watching for changes")
	index := uint64(0)
	for {
		queryOptions := &api.QueryOptions{AllowStale: true, WaitIndex: index}
		networkLogger.WithField(indexField, index).Debug("blocking")
		pairs, meta, err := s.consulKV.List(path.Join(storePath, net), queryOptions)
		if err != nil {
			networkLogger.Warn("error while watching: %s", err)
			time.Sleep(2 * time.Second) // TODO: think about backoff
			continue
		}
		if meta.RequestTime > s.consulTimeout {
			networkLogger.Debug("step watching, timeout reached")
			continue
		}
		networkLogger.Infof("changes detected, proceding to update")
		s.fillWithData(pairs, net)
		if _, ok := s.KnownNets[net]; !ok {
			networkLogger.Infof("no local hosts, stop watching")
			return
		}
		index = meta.LastIndex
	}
}

func (s *SenseDNS) boot() {
	log.Debug("Loading existing containers")
	containers, err := s.dockerClient.ListContainers(docker.ListContainersOptions{All: false})
	if err != nil {
		log.Fatalf("Error loading existing containers: %s", err)
	}
	set := make(map[string]docker.APIContainers)
	for _, c := range containers {
		set[c.ID] = c
	}
	log.Debug("Loading SenseDNS inventory")
	pairs, _, err := s.consulKV.List(path.Join(inventoryPath, s.NodeID), nil)
	if err != nil {
		log.Fatalf("Error loading inventory information: %s", err)
	}
	for _, value := range pairs {
		log.Debugf("Found item on inventory: %s - %s ", value.Key, string(value.Value))
		containerID := path.Base(value.Key)
		s.HostCache[containerID] = path.Base(path.Dir(value.Key))
		if _, ok := set[containerID]; !ok {
			log.Debugf("Container %s in on inventory but not on host: removing", containerID)
			s.deleteContainer(&docker.APIEvents{ID: containerID, Status: "deleted-when-absent"}, false)
		}
	}
	for _, c := range containers {
		s.addContainer(&docker.APIEvents{ID: c.ID, Status: "found-running"})
	}
}
