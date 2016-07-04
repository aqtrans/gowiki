#!/bin/sh
wget -O assets/bourbon.css.map http://static.jba.io/bourbon.css.map
wget -O assets/bourbon.css http://static.jba.io/bourbon.css
go build -o ./wiki && notify-send -i gopher.png -a Golang 'Golang compiled'
./wiki -d
