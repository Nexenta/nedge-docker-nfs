NexentaEdge Plugin for Docker Volumes
======================================
## Description
  This plugin provides the ability to use NexentaEdge 2.0 Clusters as backend
  devices in a Docker environment over NFS protocol.

## Configuration
Create a /etc/ndnfs folder and put your ndnfs.json config file there.
Example of a config file can be found in the ndnfs/driver folder.
  ```
  mkdir /etc/ndnfs
  cp ndnfs/driver/ndnfs.json /etc/ndnfs
  ```

## Driver Installation
After the above Prerequisites are met, use the Makefile:
  ```
  make
  ```

## Starting the daemon
If you changed any config options, you will need to restart the plugin
for changes to take effect.

  ```
  docker plugin disable nexenta/nexentaedge-nfs-plugin:stable
  docker plugin enable nexenta/nexentaedge-nfs-plugin:stable
  ```

## Usage Examples
For a list of avaialable commands run:
  ```
  docker volume --help
  ```

Here's an example of how to create a Nexenta volume using the Docker Volume
API:
  ```
  docker volume create -d ndnfs --name=testvolume -o size=1024
  ```

Now in order to use that volume with a Container you simply specify
  ```
  docker run -v testvolume:/Data --volume-driver=ndnfs -i -t ubuntu
  /bin/bash
  ```

Note that if you had NOT created the volume already, Docker will issue the
create call to the driver for you while launching the container.  The Driver
create method checks the Nexenta backend to see if the Volume already exists,
if it does it just passes back the info for the existing volume, otherwise it
runs through the create process and creates the Volume on the Nexenta
backend.
