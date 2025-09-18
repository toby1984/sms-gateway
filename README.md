# What's this

This is a Go daemon that acts as a VERY basic SMS gateway that accepts messages to be sent
via REST API and forwards those to one or more configured recipients (so no, you cannot dynamically decide 
who receives those SMS)

# Features

- REST endpoint for sending SMS
- REST endpoint for querying service status (uptime, modem status)
- Discovery of serial port interface to use based on USB vendorId and productId 
- pending messages get stored in ${dataDir}/incoming , delivered messages get stored in ${dataDir}/sent
- up to two configurable rate limits  
- failed deliveries will be retried indefinitely but with exponential back-off (just delete messages from the ${dataDir}/incoming folder to get rid of those)
- supports sending keep-alive SMS after a configurable interval has elapsed without any SMS being sent (useful to prevent mobile providers disabling prepaid cards for going unused for too long)
- tested with Huawei E3351 2G USB stick as well as E3372h-320 4G USB stick 

# Building

To compile just the Golang program, run

````
build.sh [raspi]
````

Passing 'raspi' as argument will cross-compile the program for ARM64 (Raspi 3/4/5).

# Setting up the USB stick

1. Install usb-modeswitch

````
    apt-get install usb-modeswitch
    apt-get install usb-modeswitch-data
````

2. To setup E3351 or E3372-320h support

(Tested on Raspi4 running Debian 12). All of these changes are also committed to this GIT repository inside the /etc folder.

- Create udev rule for your USB stick
````
root:/etc/udev/rules.d# cat 99-huawei.rules
# Rule for E3351
# ACTION=="add", SUBSYSTEM=="usb", ATTRS{idVendor}=="12d1", ATTRS{idProduct}=="1f01", RUN{program}+="/usr/sbin/usb_modeswitch -v 12d1 -p 1f01 -I -M '55534243123456780000000000000011062000000100000000000000000000'"
# Rule for E3372-320
ACTION=="add", SUBSYSTEM=="usb", ATTRS{idVendor}=="12d1", ATTRS{idProduct}=="1f01", RUN+="/usr/sbin/usb_modeswitch -v 12d1 -p 1f01 -M '55534243123456780000000000000011063000000100010000000000000000'"
ACTION=="add", SUBSYSTEM=="usb", ATTRS{idVendor}=="12d1", ATTRS{idProduct}=="155e", RUN+="/bin/bash -c 'modprobe option && echo 12d1 155e > /sys/bus/usb-serial/drivers/option1/new_id'"
````
-  At least for E3351 I had to activate some USB storage quirks

````
root:/etc/udev/rules.d# cat /etc/modprobe.d/huawei.conf
options usb-storage quirks=12d1:1f01:s
````

- I also had this in my usb_modeswitch.d folder (but I think it's redundant with the udev rule)

````
root@:/etc/udev/rules.d# cat /etc/usb_modeswitch.d/switch.conf
# Huawei E3372 and others
# Switch from default mass storage device mode 12d1:1f01 to ...
TargetVendor=0x12d1
TargetProduct=0x155e
MessageContent="55534243123456780000000000000011063000000100010000000000000000"
````

4. Restart udev and add systemd service, re-plug the stick if necessary

````
    udevadm control --reload
    udevadm trigger
    systemctl restart udev
    systemctl daemon-reload
    systemctl enable sms-gateway
    systemctl start sms-gateway
````

# Setting up the SMS gateway

Note that the application needs to persist some state (for the SMS card keepAlive feature) and thus that folder will need to be mounted inside the container with read-write permissions.

1. Create the following configuration file (obviously use the values that are specific to your setup)

````
[common]
# Log level, possible values are
# TRACE, DEBUG, WARN, INFO, ERROR
logLevel=DEBUG
# where to store state information
dataDirectory=/apps/sms-gateway
# debugFlags=modem_always_succeed, modem_always_fail

[modem]

# PIN to unlock SIM card
# simPin=<YOUR PIN>

# (optional) Commands to send when initializing the modem.
# Multiple commands may be separated by literal '\r' 
# (like "ATE0\rAT^CURC=0\rAT+CPOL=0")
initCmds=AT^CURC=0

# USB device ID to use for
# locating the serial device 
# (/dev/ttyUSB?) to use. 
# usbVendorId=12d1
# usbProductId=155e

# Name of modem serial port device to use.
# Either a USB device like '/dev/ttyUSB3' _OR_
# if usbVendorId and usbProductId are given, an integer index 
# describing which of the USB interfaces associated with the given USB 
# device should be used. Discovered USB interfaces get 
# sorted in ascending alphabetical order (so if discovery
# turned up /dev/ttyUSB0, /dev/ttyUSB1 and /dev/ttyUSB2,
# an 'serialPort=3' will yield /dev/ttyUSB2.
#
serialPort=/dev/ttyUSB2
# modem serial port speed
serialSpeed=115200
# how long to wait until assuming
# the modem has finished processing the current
# command and no more output is expected.
serialReadTimeoutSeconds=10

[restapi]
# IP to bind API to
bindIp=<bind IP>
port=<bind TCP port>
# HTTP basic auth user
user=<REST API USER>
# HTTP basic auth password
password=<REST API PASSWORD>

[sms]
# comma-separated list of subscriber numbers to send the SMS to.
# Note that an SMS cannot have multiple recipients so
# one SMS will be sent to each recipient (which obviously drives up costs).
recipients=<subscriber number in international format, +xxxxxx)

# (optional) Rate limit #1
# How many SMS may be sent within a given time interval.
#
# Some examples of Valid values are:
# "3/5m" = at most 3 SMS within 5 minutes
# "10/1h" = at most 10 SMS within 1 hour
# "20/3d" = at most 20 SMS within 3 days
# "100/4w" = at most 100 SMS within 4 weeks
rateLimit1=2/1h
# (optional) Rate limit #2
rateLimit2=5/1d

# Whether to send a keepAlive SMS ever so often.
#
# This might be needed if you telco provider is one of those
# that deactivate the SIM card if it goes unused for too long.
#
# Specify an interval using "32d" (=32 days), "4w" (=weeks)
keepAliveInterval=1m
keepAliveMessage=Keep-alive SMS, please ignore.
````

# Querying application status via the REST API
Assuming the service runs in 127.0.0.1, port 9999 and HTTP Basic auth credentials are "restuser:password",
you can use the following command to query the application's status:
````
curl -u "restuser:password" -H "Content-Type: application/json" http://127.0.0.1:9999/status
````
The response will look something like this:

````
{
  "operational": true,
  "network_status": "REGISTERED_HOME",
  "startup_time": "2025-09-18 08:48:15+0200",
  "uptime_in_seconds": 6
}
````

The 'operational' boolean property indicates whether sending SMS is likely to succeed because the connection to the modem is working, 
the modem's SIM card is unlocked and the modem has successfully registered with the network.  
The 'network_status' gives detail information about the modem's current connection to the network. Possible values currently are:

- NOT_REGISTERED_NOT_SEARCHING
- REGISTERED_HOME
- NOT_REGISTERED_SEARCHING
- NOT_REGISTERED_DENIED
- UNKNOWN
- REGISTERED_ROAMING

Those values directly map to the values 0-5 of the modem's response to the "AT+CREG?" command. 

# Sending an SMS via the REST API

Assuming the service runs in 127.0.0.1, port 9999 and HTTP Basic auth credentials are "restuser:password",
you can use the following command to send an SMS.
````
curl -X POST -u "restuser:restpassword" -H "Content-Type: application/json" -d '{ "message": "test" }' http://localhost:9999/sendsms
````