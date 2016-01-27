# SenseDNS

Multihost DNS for docker networks.

SenseDNS acts as a DNS for docker distributed networks. It runs on every machine on a cluster, listens to the docker.sock and stores on a distributed key/value storage (at the moment only consul is allowed) the Hostname and IP of the container and its network.

## Steps to launch

To allow multihost networks on docker make sure to follow [docker overlay networking](https://docs.docker.com/engine/userguide/networking/dockernetworks/#an-overlay-network). Next launch SenseDNS on every machine of your cluster.

```
git clone https://github.com/devopsbq/sensedns.git
cd sensedns
go build
./sensedns -c <YOUR_CONSUL_URL> -n docker
```
This will launch SenseDNS on the host, at port 53 (another port may be specified with -p option). Next we create a network.
```
docker network create -d overlay testnetwork
```

Then, if we launch a container on that network:

```
docker run -d -h redis --net testnetwork --dns <DNS_IP> --dns-search testnetwork.docker redis
```

Container addresses of the network can be resolved and reached by any other container on the same network. (By hostname)

In this example we do the next:
- '-h' to specify a hostname for the container, this hostname will be used for DNS resolution.
- '--net' to specify the network of the container.
- '--dns' to point the location of the DNS (this can be specified on DOCKER_OPTS, that way we don't need to set it into every docker run)
- '--dns-search' this is just to avoid setting the last path of the address: ping redis vs ping redis.testnetwork.docker

## How it works (distributed mode)

SenseDNS is listening to the docker socket. Every time a container is started SenseDNS will inspect it and get the container's networks. If the network
didn't exist previously, an new entry will be added to the distributed key/value storage, and that instance of SenseDNS is going to keep watching that network
for new additions. If that same network is used by a container on other node of the cluster, the SenseDNS that's running on that node will add it to the storage, as before, and will notice that is also a previous entry, so it will update its DNS table.

### Features
- Round-robin for requests
- Fault tolerant. If one SenseDNS of the cluster fails, when it recovers will update its information.
- Distributed DNS solution.
- Reactive. When a new container appears it will upgrade its entries at the moment.
- Lightweight. Doesn't use much memory (few MB of memory). It's smart enough to only listen to networks that exist on the machine where it is running, even if the cluster has others.
- Allows redirections to other DNS.
- Can be run as a binary or as a docker container: `docker run -d devopsbq/sensedns -v /var/run/docker.sock:/var/run/docker.sock`.
- Ready for production!

### Options

Can be specified via command line argument or environment variable.

- '-c' or CONSUL_URL to set the consul url. Defaults to "127.0.0.1:8500"
- '-l' or LOG_LEVEL to set the level of log. Defaults to "INFO"
- '-a' DNS_LISTEN_ADDRESS to set the address where to listen. Defaults to "0.0.0.0"
- '-p' DNS_LISTEN_PORT to set the port where to listen. Defaults to"53"
- '-r' REDIRECT_DNS to set a DNS to forward requests that can't be resolved. Defaults to "8.8.8.8:53"
- '-n' NETWORK_TLD to set the top-level domain. Defaults to ".sensedns"

### Demo

[![asciicast](https://asciinema.org/a/96wmmiw7mzpvgxokaoo1p96ko.png)](https://asciinema.org/a/96wmmiw7mzpvgxokaoo1p96ko)

#### TO-DO
- Design interface to allow etcd and others
- Improve documentation
- Remove host network and other networks from sensedns resolution
- Implement a method to remove other dns data if can't be reached
- Refactor and improve DNS

#### Think
- Use file to store things if consul can't be reached?


This is a work in progress, it may contain some little bugs. Beside this, we have this running on our cluster without any problem to the date.
