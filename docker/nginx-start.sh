#!/bin/bash
## remove the default template 
/usr/local/bin/gotemplate -f /usr/local/etc/site-config.conf -o /etc/nginx/sites-enabled/default 
nginx -g "daemon off;"