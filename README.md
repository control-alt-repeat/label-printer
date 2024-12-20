# Label Printer

<p style="color:orange; font-weight:bold;">⚠️Warning: This is an active repo; expect breaking changes and use for reference only!⚠️</p>

Wraps [brother_ql](https://github.com/pklaus/brother_ql) in a Go server for remote operation.  It is tightly coupled to AWS Parameter Store at the moment. I probably won't have time to help you with that if you get stuck!

## Prerequisites
- [Podman](https://docs.podman.io/en/latest/)
- podman-compose
- A compatible printer (see [brother_ql's README](https://github.com/pklaus/brother_ql))
- Maybe others but I need to install on a fresh system to find out

## Install
### Debian (Tested on Ubuntu)

First create an environment file and add AWS credentials to it:
```
# filename: etc/default/aws-env
AWS_ACCESS_KEY_ID=yours
AWS_SECRET_ACCESS_KEY=yours
AWS_DEFAULT_REGION=yours
AWS_REGION=yours
```
Then run in the install script:
```shell
git clone https://github.com/control-alt-repeat/label-printer.git
cd label-printer
install-label-printer-server.sh
```

### Todo

- Request authentication/validation
- Auto-scaling of PNG to label
