FROM scratch
MAINTAINER devopsbq <devops@bq.com>

ENV CONSUL_URL=127.0.0.1:8500
ENV CONSUL_TIMEOUT=5m
ENV REDIRECT_DNS=8.8.8.8:53
ENV NETWORK_TLD=sensedns

EXPOSE 53
EXPOSE 53/udp

WORKDIR /dns
COPY sensedns /dns/sensedns
ENTRYPOINT ["/dns/sensedns"]
