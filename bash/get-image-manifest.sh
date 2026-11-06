#!/bin/bash
#
# Shell script for getting image manifest from v2 registry
#
# Tested on Debian 8, curl 7.38.0, jq-1.5
#

set -e -u

# Default tag is latest
tag=${TAG:-latest}

getLogin() {
    read -r -p "Please enter username: " username
    read -r -p "Please enter reponame: " repo
    reponame=${username}/${repo}
    if [ -n "$username" ]; then
        read -r -s -p "password: " password
        echo
    fi
}

getBearerToken() {
    local HEADERS=""

    if [ -n "$username" ]; then
        HEADERS="Authorization: Basic $(echo -n "${username}:${password}" | base64)"
    fi
    echo "[+] Logging in"
    curl -s -H "$HEADERS" "https://auth.docker.io/token?service=registry.docker.io&scope=repository:${reponame}:pull" | jq '.token' -r > token
    echo "[+] Got Bearer token"
}

getManifestV2() {
    local ACCEPT="Accept: application/vnd.docker.distribution.manifest.v2+json"
    local REGISTER_URI="https://registry-1.docker.io/v2"
    curl -s -H "${ACCEPT}" -H "Authorization: Bearer $(cat token)" "${REGISTER_URI}/${reponame}/manifests/${tag}" -o manifest.json
    echo
    echo "[+] Got Manifest in 'manifest.json'"
}

doClean() {
    rm -f token
    echo "[+] Token file removed"
}

getLogin
getBearerToken "${reponame}"
getManifestV2
doClean
