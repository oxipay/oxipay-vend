
# Oxipay Vend Application Proxy


## Integrate your Vend Terminal with Oxipay

## Development

This assumes you have the repo cloned to $GOPATH/src/github.com/oxipay/oxipay-vend.

### Dependencies
* Go (tested with version 1.10)
* Glide (https://glide.sh/)
* A MariaDB or MySQL Database. Other db's can be supported easily however MariaDB is fast and easy to replicate. A docker-compose file exists which can be used for testing. 

```$ glide up ```


The application requires a configuration file. A default config file is located in configs/vendproxy.json however by default the application will look in /etc/vendproxy/vendproxy.json. 

In order to change this behaviour to make development more convenient set the environment variable DEV to true

```$ export DEV=true```


### Build 

```$ glide install```

```$ cd cmd; go build ./vendproxy.go ```

### Docker Build

* Assumes you have the AWS-CLI installed and configured
* Assumes you have docker-compose installed


#### Get the login command for docker to register with ECS
``` $(aws ecr get-login --no-include-email --region <ecs region>) ```

* Execute the returned command (as long as it looks sane)

#### Build

```
    $ cd docker
    $ docker-compose build
    $ docker-compose push

```


## Executing

```$ ./vendproxy ```

```
$:~/go/src/github.com/vend/peg/cmd$ ./vendproxy 
{"level":"info","msg":"Attempting to connect to database user:password@tcp(172.18.0.2)/vend?parseTime=true\u0026loc=Local \n","time":"2018-08-23T17:10:55+09:30"}
{"level":"info","msg":"Starting webserver on port 5000 \n","time":"2018-08-23T17:10:55+09:30"}

```


### Production Setup 

#### Configuration

Ensure that the following settings are changed in your production configuration or in a vendproxy.env file if deploying using docker.

* database.username
* database.password
* session.secret (used to encrypt session info)
* oxipay.gatewayurl (should be set to the prod end point)



### Deployment

## Licenses
- [MIT License](https://github.com/vend/peg/blob/master/LICENSE)
- [Google Open Source Font Attribution](https://fonts.google.com/attribution)
