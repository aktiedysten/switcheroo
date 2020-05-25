#!/usr/bin/env bash

set -e

cd $(dirname $0)

export GOPATH=$(realpath $(pwd)/../..)

go build server.go
go build runner.go
go build cleanup.go

echo
echo "***************************************************************************"
echo "***                 RUNS FOREVER: PRESS CTRL+C TO QUIT                  ***"
echo "***************************************************************************"
echo "*** Meanwhile you can also try these in another shell:                  ***"
echo "***    curl 'http://127.0.0.1:9999?s=250'                               ***"
echo "***    sudo iptables -L -n -t nat --line-numbers                        ***"
echo "***************************************************************************"
echo

./runner
