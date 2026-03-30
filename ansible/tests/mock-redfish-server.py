#!/usr/bin/env python3
"""
Mock Dell iDRAC Redfish API server for testing discover-hardware role.
Simulates 3 hosts on ports 8440, 8441, 8442.

Usage:
  python3 ansible/tests/mock-redfish-server.py &
  # Then run the playbook against localhost:8440,8441,8442
"""

import http.server
import json
import ssl
import threading
import sys
import os

HOSTS = [
    {
        "port": 8440,
        "serial": "SN-001",
        "nics": [
            {"id": "NIC.Integrated.1-1", "mac": "b0:26:28:e8:87:01", "speed": 25000, "link": "LinkUp"},
            {"id": "NIC.Integrated.1-2", "mac": "b0:26:28:e8:87:02", "speed": 25000, "link": "LinkUp"},
            {"id": "NIC.Slot.3-1", "mac": "04:7b:cb:3e:9a:01", "speed": 100000, "link": "LinkUp"},
            {"id": "NIC.Slot.3-2", "mac": "04:7b:cb:3e:9a:02", "speed": 100000, "link": "LinkUp"},
            {"id": "iDRAC.Embedded.1-1", "mac": "d0:8e:79:01:01:01", "speed": 1000, "link": "LinkUp"},
        ],
    },
    {
        "port": 8441,
        "serial": "SN-002",
        "nics": [
            {"id": "NIC.Integrated.1-1", "mac": "b0:26:28:e8:88:01", "speed": 25000, "link": "LinkUp"},
            {"id": "NIC.Integrated.1-2", "mac": "b0:26:28:e8:88:02", "speed": 25000, "link": "LinkUp"},
            {"id": "NIC.Slot.3-1", "mac": "04:7b:cb:3e:9b:01", "speed": 100000, "link": "LinkUp"},
            {"id": "NIC.Slot.3-2", "mac": "04:7b:cb:3e:9b:02", "speed": 100000, "link": "LinkUp"},
            {"id": "iDRAC.Embedded.1-1", "mac": "d0:8e:79:02:02:02", "speed": 1000, "link": "LinkUp"},
        ],
    },
    {
        "port": 8442,
        "serial": "SN-003",
        "nics": [
            {"id": "NIC.Integrated.1-1", "mac": "b0:26:28:e8:89:01", "speed": 25000, "link": "LinkUp"},
            {"id": "NIC.Integrated.1-2", "mac": "b0:26:28:e8:89:02", "speed": 25000, "link": "LinkUp"},
            {"id": "NIC.Slot.3-1", "mac": "04:7b:cb:3e:9c:01", "speed": 100000, "link": "LinkUp"},
            {"id": "NIC.Slot.3-2", "mac": "04:7b:cb:3e:9c:02", "speed": 100000, "link": "LinkUp"},
            {"id": "iDRAC.Embedded.1-1", "mac": "d0:8e:79:03:03:03", "speed": 1000, "link": "LinkUp"},
        ],
    },
]


def make_handler(host_config):
    """Create a request handler for a specific mock host."""

    class RedfishHandler(http.server.BaseHTTPRequestHandler):
        def log_message(self, format, *args):
            pass  # Suppress request logging

        def do_GET(self):
            # Check basic auth
            auth = self.headers.get("Authorization", "")
            if not auth:
                self.send_response(401)
                self.end_headers()
                return

            response = self.route()
            if response is None:
                self.send_response(404)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps({"error": "Not Found"}).encode())
                return

            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(response).encode())

        def route(self):
            path = self.path

            if path == "/redfish/v1/Systems/System.Embedded.1":
                return {
                    "Id": "System.Embedded.1",
                    "Model": "PowerEdge R750",
                    "Manufacturer": "Dell Inc.",
                    "SerialNumber": host_config["serial"],
                    "ProcessorSummary": {
                        "Count": 2,
                        "CoreCount": 32,
                        "Model": "Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz",
                    },
                    "MemorySummary": {"TotalSystemMemoryGiB": 512},
                }

            if path == "/redfish/v1/Systems/System.Embedded.1/EthernetInterfaces":
                members = [
                    {"@odata.id": f"/redfish/v1/Systems/System.Embedded.1/EthernetInterfaces/{nic['id']}"}
                    for nic in host_config["nics"]
                ]
                return {"Members": members, "Members@odata.count": len(members)}

            if path.startswith("/redfish/v1/Systems/System.Embedded.1/EthernetInterfaces/"):
                nic_id = path.split("/")[-1]
                for nic in host_config["nics"]:
                    if nic["id"] == nic_id:
                        return {
                            "Id": nic["id"],
                            "Name": nic["id"],
                            "MACAddress": nic["mac"],
                            "SpeedMbps": nic["speed"],
                            "LinkStatus": nic["link"],
                            "@odata.id": path,
                        }

            if path == "/redfish/v1/Systems/System.Embedded.1/Storage":
                return {
                    "Members": [
                        {"@odata.id": "/redfish/v1/Systems/System.Embedded.1/Storage/RAID.Integrated.1-1"},
                    ],
                    "Members@odata.count": 1,
                }

            if path == "/redfish/v1/Systems/System.Embedded.1/Storage/RAID.Integrated.1-1":
                return {
                    "Id": "RAID.Integrated.1-1",
                    "Name": "PERC H755 Front",
                    "Drives": [
                        {"@odata.id": "/redfish/v1/Systems/System.Embedded.1/Storage/RAID.Integrated.1-1/Drives/Disk.Bay.0:Enclosure.Internal.0-1"},
                        {"@odata.id": "/redfish/v1/Systems/System.Embedded.1/Storage/RAID.Integrated.1-1/Drives/Disk.Bay.1:Enclosure.Internal.0-1"},
                        {"@odata.id": "/redfish/v1/Systems/System.Embedded.1/Storage/RAID.Integrated.1-1/Drives/Disk.Bay.2:Enclosure.Internal.0-1"},
                    ],
                }

            if "Drives/Disk.Bay" in path:
                bay = path.split("Disk.Bay.")[1].split(":")[0]
                serial_prefix = host_config["serial"].replace("SN-", "")
                return {
                    "Id": f"Disk.Bay.{bay}:Enclosure.Internal.0-1",
                    "Name": f"SSD {bay}",
                    "MediaType": "SSD",
                    "Protocol": "NVMe",
                    "CapacityBytes": 960197124096 if bay == "0" else 1920394248192,
                    "SerialNumber": f"PHAB{serial_prefix}0{bay}",
                    "Model": "Dell Ent NVMe P5600 MU U.2 960GB" if bay == "0" else "Dell Ent NVMe P5600 MU U.2 1.92TB",
                }

            return None

    return RedfishHandler


def generate_self_signed_cert():
    """Generate a temporary self-signed cert for HTTPS."""
    import tempfile
    import subprocess

    cert_dir = tempfile.mkdtemp()
    cert_file = os.path.join(cert_dir, "cert.pem")
    key_file = os.path.join(cert_dir, "key.pem")

    subprocess.run(
        [
            "openssl", "req", "-x509", "-newkey", "rsa:2048",
            "-keyout", key_file, "-out", cert_file,
            "-days", "1", "-nodes",
            "-subj", "/CN=localhost",
        ],
        capture_output=True,
        check=True,
    )
    return cert_file, key_file


def start_server(host_config, cert_file, key_file):
    """Start an HTTPS server for one mock host."""
    handler = make_handler(host_config)
    server = http.server.HTTPServer(("127.0.0.1", host_config["port"]), handler)

    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(cert_file, key_file)
    server.socket = ctx.wrap_socket(server.socket, server_side=True)

    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server


def main():
    cert_file, key_file = generate_self_signed_cert()

    servers = []
    for host in HOSTS:
        srv = start_server(host, cert_file, key_file)
        servers.append(srv)
        print(f"Mock iDRAC running on https://127.0.0.1:{host['port']}")

    print(f"\n{len(HOSTS)} mock iDRACs ready.")
    print("Test with:")
    host_list = ", ".join(f'"127.0.0.1:{h["port"]}"' for h in HOSTS)
    print("  ansible-playbook ansible/playbooks/discover-and-create-cluster.yaml \\")
    print(f'    -e \'{{"idrac_hosts": [{host_list}]}}\' \\')
    print("    --ask-vault-pass")
    print(f"\nPress Ctrl+C to stop.")

    try:
        threading.Event().wait()
    except KeyboardInterrupt:
        print("\nShutting down...")
        for srv in servers:
            srv.shutdown()


if __name__ == "__main__":
    main()
