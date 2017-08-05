#!/bin/bash
# Deploy an app, first backing up the folder
# scp'ing the new app, then copying the old data/ folder over

# Full user@host SSH login; ex golang@frink.es.gy
SSHLOGIN=$1
# Name of the systemd service; ex golang-wiki
SERVICENAME=$2
# App directory name; should be just the app name
DIR=$3

if [[ -z "$SSHLOGIN" ]]; then
    exit 1
fi

if [[ -z "$SERVICENAME" ]]; then
    exit 1
fi

if [[ -z "$DIR" ]]; then
    exit 1
fi

# Backup old app
ssh $SSHLOGIN mv ~/$DIR{,.old}
# rsync to fresh folder
rsync -av --exclude data/ --exclude vendor/ --exclude http.log ./ $SSHLOGIN:~/$DIR
# Copy data/ from old to new
ssh $SSHLOGIN cp -rp ~/$DIR.old/data ~/$DIR/
# Restart 
ssh $SSHLOGIN sudo systemctl restart $SERVICENAME