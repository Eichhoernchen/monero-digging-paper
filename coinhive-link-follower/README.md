# Coinhive-Link-Follower

This thing follows coinhive links.


## Building
It is a bit tricky to compile since it requires the cryptolibraries from the monero project.

To this end, you first need to get the monero deamon from https://github.com/monero-project/monero
So follow all instruction there to get all dependencies to compile it.
You need to compile it but you need to make sure to compile it with library support so that we can link our tool to it.

To this end, modify the makefile an include in some target the `-DBUILD_SHARED_LIBS=ON`,
so for example, change the debug-all target to have it switched on (it should already have it on the there) and then compile the debug-all target.

Next, since I have no clue how to include environment variables in the go build process (fix this and create a pull request), you have to change the line 3 in hashing/hashing.go

```
// #cgo CFLAGS: -std=c11 -D_GNU_SOURCE -I/Users/jan/projects/monero/src -I/Users/jan/projects/monero/contrib/epee/include
```

Make sure the include path points to your local monero src directory and contrib/epee/include directory.
Also copy over the libcncrypto.{so/dylib} to the hashing directory, or add a -L flag.

Now you should be able to compile it with go build

## Running this thing

The program wants a json input from stdin, and it will produce a json output to stdout.

It requires this format:

```
{"postfix":"ABCDEFG","url":"https://cnhv.co/ABCDEFG","token":"zzym7ThSYT4Om05EAQfETAaVjMIeJRV9","target":1024}
```

You can get the parameters from the coinhive link's HTML source.

The output will contain the actual website you wanted to go to.

You can also adjust the number of parallel resolutions.... if you have enough CPU power

