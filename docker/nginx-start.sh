#!/bin/bash
/usr/local/bin/gotemplate  -f /etc/nginx/sites-available/default > /etc/nginx/sites-enabled/default 

nginx -g "daemon off;"