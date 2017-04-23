#!/bin/sh
cd scss/
npm install
npm install bower
node_modules/.bin/bower --allow-root install
npm build
node_modules/.bin/gulp sass
cd ../