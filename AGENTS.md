## Introduction 
We are a terminal based music player. The core concept is to be simple, minimal and easy to use. 

## Tech Stack
- All written in Go
- Written for Linux (only Linux as of now)

## Code Style Guidelines
Assume this development host has no sound card. Make the sound support generic for all Linux hosts and assume you will not be able to test the sound on this host.

HARD RULE: When the binary is run with `-v`, it MUST print the latest git tag version (e.g. `v0.1.18`), never a dev/placeholder string like `muzak321 dev`. If `VERSION` is not set at link time, fall back to `git describe --tags --always`.


 
