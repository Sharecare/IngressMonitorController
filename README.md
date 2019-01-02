# IngressMonitorController [![Build Status](https://drone.admin.sharecare.com/api/badges/Sharecare/IngressMonitorController/status.svg)](https://drone.admin.sharecare.com/Sharecare/IngressMonitorController)

IngressMonitorController is forked from an open source project to serve
as the base for Maestro's automatic healthcheck monitoring
solution. On its own, it listens to the event feed from Kubernetes for
Ingresses, and makes calls to update uptime robot monitors
accordingly.

Our fork exists to handle a few bugs and specific use cases that might
not be generally applicable:

- url encoding a healthcheck with ampersands (&) in it
- customizing the UptimeRobot monitor interval duration


## Installation

This is a go project, so it must be cloned to your $GOHOME directory
accordingly.

```
$ glide up -v
$ make build
```

## Inputs

Pushing a tag to the maestro branch will create a new docker image as
per this project's Drone pipeline, linked in the badge above.

## Outputs

A docker image that serves as the base for the
`Sharecare/maestro-monitor-controller` project.

## Dependency or Notable Interaction

NA

## More Information

NA
