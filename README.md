# qubesome-cli

Image Storage


qubesome import --profile="personal" ghcr.io/qubesome/chrome:latest
qubesome import --profile="personal" --name="Qubesome Chrome" ghcr.io/qubesome/chrome:latest

qubesome run 
    --camera --audio --x11 --gpu
    --network="bridge"
    --profile="personal" ghcr.io/qubesome/chrome:latest chrome


profile
- allowed paths, devices

Security guarantees

Out of scope
