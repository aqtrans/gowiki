#!/bin/bash
docker run -it --rm --name running-wiki --publish 3001:3001 -v /home/aqtrans/production_apps/wiki/conf.json:/go/src/app/conf.json:Z \
 -v /home/aqtrans/production_apps/wiki/http.log:/go/src/app/http.log:Z \
 -v /home/aqtrans/production_apps/wiki/auth.db:/go/src/app/auth.db:Z \
 -v /home/aqtrans/production_apps/wiki/md:/go/src/app/md:Z \
 my-wiki /bin/bash
