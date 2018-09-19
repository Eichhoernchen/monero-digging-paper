# Coinhive Bock Grep

These tools are used to estimate coinhive's revenue.

To this end, we requires 3 tools, build the top level and the two folders using `go build`.

The first (top level directory), connects to coinhive to get blocks for POW computation and just dumps them on stdout.

You can pass the coinhive endpoint as a `url` parameter.

Do this for all coinhive endpoints.

Collect the data to a single file.

You can check out the uniq_blob_* scripts to see how you can reduce duplicated data.

Now pipe this file to `get_coinhive_blocks.sh`, you need `jq`.



You require a monero deamon with a synchronized blockchain for this, the shell script assumes it is running on localhost, but you can change it to what ever you want. 
But be aware we are running a lot of queries to the deamon.

Internally, the script uses the decode_blob and get_merkles tools and produces actually mined blocks as the output.
