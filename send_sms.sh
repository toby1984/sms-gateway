#!/bin/bash
curl -X POST -u "restuser:myfunkypassword" -H "Content-Type: application/json" -d '{ "message": "test" }' http://localhost:9999/sendsms
