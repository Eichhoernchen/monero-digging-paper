#!/bin/bash


read -r first
start_block=$(echo $first | ./decode_blob/decode_blob | jq -cr '.pow.previous')
start_height=$(curl -s -X POST http:/localhost:18081/json_rpc -d '{"jsonrpc":"2.0","id":"0","method":"getblock","params":{"hash": "'${start_block}'"}}' -H 'Content-Type: application/json' | jq -cr '.result.block_header.height')

cat <(echo $first) - | sort -S10G -u | ./decode_blob/decode_blob | cat - <(./get_merkles/get_merkles -start_height ${start_height}) -monero-node "localhost:18081" | jq -cs 'group_by(if .merkle_tree_root != null then .merkle_tree_root else .pow.merkle_tree_root end) | .[]' | jq -c 'if length == 2 then . else empty end | reduce .[] as $item ({}; . + (if $item.hash != null then {"block_infos": $item} else {"pow": $item.pow, "job": $item.params} end))'
