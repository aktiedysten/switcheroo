# What?

Zero-downtime / no-interruption deployment of Golang TCP servers on Linux.

When using Switcheroo, you get an executable that ostensibly starts listening
on a port of your choice, say 8080. Then when you need deploy a new executable,
simply execute it, and it will replace the old executable; traffic continues to
flow uninterrupted on port 8080; new connections are routed to your new
executable; and in-flight connections to old executables are allowed to finish.
When starting the new executable, old executables receive a `SIGTERM` (kill)
which should be used to shut down in a graceful manner.

Internally, however, your server is not listening on port 8080; it might be
listening on port 40404, and `iptables` is used to route traffic from port 8080
to port 40404. The no-interruption guarantee is entirely due to iptables;
adding a new rule for port 8080 overrides older rules; and deleting rules
doesn't drop connections, it simply prevents the rule from "firing" when new
connections are made. Thanks iptables!


# Example

See `test/server.go`


# Porting to other languages

Since it's all just a bunch of `iptables` commands, it can easily be ported to
other languages. It's even somewhat possible to do it from the "outside", e.g.
by having a "switcheroo process" that wraps/runs your server on a chosen
dynamic port (but then atomic port allocation is no longer possible). Also,
it's sometimes more convenient to have one binary, instead of two. If you want
to port it, have a look at the code; it's not complicated.


# Porting to other operating systems

Sorry, I can't help you here; this library hinges on subtle details about
`iptables` internals.


# Why not haproxy?

Last time I checked, they still hadn't solved this particular problem; there's
a small window where connections might be dropped when switching to a new
server. Also, haproxy is software you must install. `iptables`, on the other
hand, is already installed; you can't not have it installed :)


# Caveats

 - Only runs on Linux (because it depends on `iptables`)
 - Only runs with root-privileges, optionally via sudo (again due to
   `iptables`)
 - It's not safe to use if other processes manipulate the affected iptables
   chains (`OUTPUT` for localhost routing, `PREROUTING` for network routing).
   This is a race condition; in order to delete iptables rules, we must refer
   to these rules by a number that changes when other rules are added or
   deleted, so in the time between listing rules and deleting them, things
   could change!
