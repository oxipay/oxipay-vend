#!/bin/bash
envsubst <  /etc/nginx/sites-available/default.conf > /etc/nginx/sites-enabled/default   
nginx -g "daemon off;"