# Remote Development using SSH, Visual Studio Code and Kubernetes

[![Go Report Card](https://goreportcard.com/badge/github.com/digitorus/remote-vsc-k8s)](https://goreportcard.com/report/github.com/digitorus/remote-vsc-k8s)
[![Coverage Status](https://codecov.io/gh/digitorus/remote-vsc-k8s/branch/master/graph/badge.svg)](https://codecov.io/gh/digitorus/remote-vsc-k8s)
[![GoDoc](https://godoc.org/github.com/digitorus/remote-vsc-k8s?status.svg)](https://godoc.org/github.com/digitorus/remote-vsc-k8s)

[![Docker Stars](https://img.shields.io/docker/stars/digitorus/remote-vsc-k8s.svg)](https://hub.docker.com/r/digitorus/remote-vsc-k8s)
[![Docker Pulls](https://img.shields.io/docker/pulls/digitorus/remote-vsc-k8s.svg)](https://hub.docker.com/r/digitorus/remote-vsc-k8s)
[![Docker Automated build](https://img.shields.io/docker/automated/digitorus/remote-vsc-k8s)](https://hub.docker.com/r/digitorus/remote-vsc-k8s)
[![Docker Cloud Build Status](https://img.shields.io/docker/cloud/build/digitorus/remote-vsc-k8s)](https://hub.docker.com/r/digitorus/remote-vsc-k8s/builds)

An SSH proxy that created user dedicated Kubernetes pods for [Visual Studio Code Remote (SSH)](https://code.visualstudio.com/docs/remote/ssh) on demand using Kubernetes. 

The pod image can be confired to deploy your own pre-configured image holding all your favorite tooling.

#### Code status

The current version (alpha) is based on a proof of concept, we are open for all contributions, specifically:
- Big code cleanup (add godoc documentation, fix lint errors, split functions, etc)
- Split code in multiple files
-- SSH specific
-- Kubernetes specific
- Add support for SSH agent forwarding
- Add more implementation tests
- ...
