# What's this

This is a Go daemon that acts as a VERY basic SMS gateway together with a Huawei E3351 HSPDA USB stick.

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

2. Create a udev rule to switch the USB stick into USB modem mode (otherwise it will automatically come up in HiLink mode as an ethernet device)

Put the following into /etc/udev.d/99-huawei-e3351.rules 

````
    # ACTION=="add" , ATTRS{idVendor}=="12d1", ATTRS{idProduct}=="1f01", RUN+="/sbin/usb_modeswitch -v 12d1 -p 1f01 -I -M '55534243123456780000000000000011062000000100000000000000000000' "
    # taken & converted from https://community.home-assistant.io/t/modem-huawei-e3531-from-storage-to-serial-modem-mode-procedure-i-used/523542
    ACTION=="add", SUBSYSTEM=="usb", ATTRS{idVendor}=="12d1", ATTRS{idProduct}=="1f01", RUN{program}+="/usr/sbin/usb_modeswitch -v 12d1 -p 1f01 -I -M '55534243123456780000000000000011062000000100000000000000000000'"
````

3. Restart udev and afterwards re-plug the stick if necessary

````
    systemctl restart udev
````

# Setting up the SMS gateway

This Go SMS gateway comes conveniently packaged as a Docker image, the only thing that needs customizing is the configuration file that will go into the application's data directory.

Note that the application needs to persist some state (for the SMS card keepAlive feature) and thus that folder will need to be mounted inside the container with read-write permissions.

1. Create the following configuration file (obviously use the values that are specific to your setup)

````
    [modem]
    # SIM card PIN
    pin=1234
    serialDevice=/dev/tty/USB1
    
    [restapi]
    port=9999
    password=myfunkypassword
    
    [sms]
    # comma-separated list of subscriber numbers to send the SMS to.
    # Note that an SMS cannot have multiple recipients so
    # one SMS will be sent to each recipient (which obviously drives up costs).
    recipients=+491234
    
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
    rateLimit2=10/1d
    
    # Whether to send a keepAlive SMS ever so often.
    #
    # This might be needed if you telco provider is one of those 
    # that deactivate the SIM card if it goes unused for too long.
    #
    # Specify an interval using "32d" (=32 days), "4w" (=weeks)
    # keepAliveInterval=4w
    # keepAliveMessage=The message to send 
````
