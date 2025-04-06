#! /usr/bin/env bash



make build

./build/enma hotload --daemon "./misc/playground/daemon.sh" --build "md5 ./misc/playground/test.txt" --watch-dir "./misc/playground/"


