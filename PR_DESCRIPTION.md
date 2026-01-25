# Fix: Handle nil connection from open channel safely

## Description
This PR addresses Issue #24 where a nil connection received from the `conns` channel could strictly return `ErrClosed`, potentially masking transient issues or race conditions during pool sizing/resizing.

The changes implement the "comma-ok" idiom for channel reception:
1. Explicitly checks if the channel is closed (`!ok`) and returns `ErrClosed` in that case.
2. If the channel is open but the received value is `nil` (which shouldn't happen in a healthy pool but acts as a safeguard), it now `continue`s the loop instead of returning an error, allowing the pool to attempt to retrieve another valid connection or create a new one.

This robust handling ensures that `nil` values don't prematurely terminate `Get()` calls when the pool is still operational.

## Changes
- Modified `channel.go`: Updated `Get()` method to safe-guard against nil connections from open channels.
- Added `channel_race_test.go`: Added a regression test `TestPoolReleaseRace` to verify behavior during concurrent `Get()` and `Release()` operations.

## Test Plan
- [x] Run `go test -v .` to verify all tests pass, including the new regression test.
- [x] Verified that `TestPoolReleaseRace` consistently passes and correctly identifies `ErrClosed` when the pool is released.
