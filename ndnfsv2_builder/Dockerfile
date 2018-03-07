FROM alpine

RUN apk update

RUN mkdir -p /run/docker/plugins /var/lib/ndnfs/state 

COPY ndnfs ndnfs

CMD ["bin/sh"]
