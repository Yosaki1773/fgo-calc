#!/bin/zsh
python3 data.py
python3 ce.py
cd backend
go build .
./fgo-calc-backend -config config.dev.json

