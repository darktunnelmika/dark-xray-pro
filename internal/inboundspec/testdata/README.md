# Synthetic inbound fixtures

`protocols.json` contains one private test specification for each of the fourteen supported protocols. Every credential in this directory is public, synthetic test material. Never deploy these keys or certificates. The names use `.test`; tests render or parse configuration only and must not contact those endpoints. Account identities are injected by tests rather than accepted in the specification.
