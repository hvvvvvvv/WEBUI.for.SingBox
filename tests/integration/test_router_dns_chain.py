"""Pure protocol/topology checks; no listeners, core, or network operations."""
import importlib.util
import json
import pathlib
import struct
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("dns_chain", pathlib.Path(__file__).with_name("router-dns-chain.py"))
CHAIN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHAIN)


class DNSChainTests(unittest.TestCase):
    def test_dns_transaction_and_distinct_answers(self):
        request = CHAIN.query("native-lan-1.fixture.test")
        for address in (CHAIN.FORWARDED, CHAIN.INTERCEPTED):
            response = CHAIN.answer(request, address)
            self.assertEqual(CHAIN.address_from_response(request, response), address)
            with self.assertRaises(AssertionError):
                CHAIN.address_from_response(request, b"xx" + response[2:])

    def test_truncated_or_compressed_question_is_rejected(self):
        request = CHAIN.query("native.fixture.test")
        for packet in (request[:12], request[:-1], request[:12] + b"\xc0\x0c" + request[-4:]):
            with self.assertRaises(ValueError):
                CHAIN.question(packet)
        with self.assertRaises(ValueError):
            CHAIN.answer(CHAIN.query("outside.example.com"), CHAIN.FORWARDED)

    def test_aaaa_is_empty_noerror_with_original_question_and_id(self):
        request = CHAIN.query("probe.fixture.test", 28)
        response = CHAIN.answer(request, CHAIN.FORWARDED)
        self.assertEqual(response[:2], request[:2])
        self.assertEqual(struct.unpack_from("!HHHHH", response, 2), (0x8180, 1, 0, 0, 0))
        self.assertEqual(response[12:], request[12:])
        with self.assertRaises(ValueError):
            CHAIN.answer(CHAIN.query("probe.fixture.test", 16), CHAIN.FORWARDED)

    def test_only_complete_independent_tcp_probe_is_accepted(self):
        def event(kind, query_id, connection):
            return {"qtype": kind, "qclass": 1, "id": query_id, "connection_id": connection}
        actual = event(1, 1, "actual")  # low multiplexed IDs are not probe IDs by themselves
        probe_a, probe_aaaa = event(1, 1, "probe"), event(28, 2, "probe")
        self.assertEqual(CHAIN.core_queries([actual]), 0)
        self.assertEqual(CHAIN.core_queries([probe_a, actual, probe_aaaa]), 1)
        with self.assertRaises(CHAIN.PendingReuseProbe):
            CHAIN.core_queries([probe_a, actual])
        for entries in ([actual, actual], [actual, probe_aaaa], [probe_a, probe_aaaa],
                        [actual, probe_a, probe_aaaa, dict(actual, connection_id="retry")],
                        [actual, probe_a, dict(probe_aaaa, connection_id="wrong-connection")],
                        [actual, probe_a, dict(probe_aaaa, id=3)],
                        [actual, probe_aaaa, probe_a],
                        [probe_a, probe_aaaa, dict(actual, connection_id="probe")]):
            with self.subTest(entries=entries), self.assertRaises(AssertionError):
                CHAIN.core_queries(entries)

    def test_mode_matrix_covers_distinct_linux_paths(self):
        for mode in ("native", "hijack"):
            self.assertEqual(CHAIN.expected(mode, False, "lan"),
                             (CHAIN.FORWARDED, ["dnsmasq", "adguard", "upstream53"]))
            self.assertEqual(CHAIN.expected(mode, False, "attached"), (CHAIN.INTERCEPTED, ["core1053"]))
            for redirect in (False, True):
                self.assertEqual(CHAIN.expected(mode, redirect, "loopback"),
                                 (CHAIN.FORWARDED, ["adguard", "upstream53"]))
        self.assertEqual(CHAIN.expected("native", True, "attached"), (CHAIN.FORWARDED, ["upstream53"]))
        self.assertEqual(CHAIN.expected("hijack", True, "lan"), (CHAIN.INTERCEPTED, ["core1053"]))

    def test_prepare_keeps_original_and_uses_separate_cache(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            source = root / "source.json"
            config = {"inbounds": [{"type": "tun", "dns_mode": "hijack", "auto_redirect": False,
                                    "route_exclude_address": ["10.204.0.0/24"], "include_interface": ["lan0"]}],
                      "experimental": {"cache_file": {"path": "original.db"}}, "dns": {},
                      "services": [{"type": "api", "secret": "fixture-only-secret"}]}
            source.write_text(json.dumps(config))
            CHAIN.prepare(source, root / "case", "native", True)
            generated = json.loads((root / "case/config.json").read_text())
            self.assertEqual(json.loads(source.read_text()), config)
            self.assertEqual(generated["inbounds"][0]["dns_mode"], "native")
            self.assertTrue(generated["inbounds"][0]["auto_redirect"])
            self.assertEqual(generated["experimental"]["cache_file"]["path"], str(root / "case/cache.db"))
            self.assertNotIn("fixture-only-secret", (root / "case/config.redacted.json").read_text())
            with self.assertRaises(FileExistsError):
                CHAIN.prepare(source, root / "case", "hijack", False)

    def test_verifier_rejects_extra_hop_and_dns_detour_bypass(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            samples, events = [], {role: [] for role in ("dnsmasq", "adguard", "upstream53", "core1053")}
            for path in ("lan", "attached", "loopback"):
                for transport in ("udp", "tcp"):
                    address, hops = CHAIN.expected("hijack", True, path)
                    domain = f"{path}-{transport}.fixture.test"
                    request_id = 100 + len(samples)
                    samples.append({"domain": domain, "path": path, "transport": transport,
                                    "mode": "hijack", "auto_redirect": True, "answer": address,
                                    "expected_hops": hops, "request_id": request_id})
                    for role in hops:
                        peer = {"adguard": "127.0.0.1", "upstream53": "10.204.0.1", "core1053": CHAIN.UPSTREAM}[role]
                        events[role].append({"domain": domain, "transport": "tcp" if role == "core1053" else transport,
                                             "peer": peer, "qtype": 1, "qclass": 1, "id": request_id,
                                             "connection_id": "actual-" + domain})
            first = events["core1053"][0]
            events["core1053"] += [dict(first, qtype=kind, id=query_id, connection_id="reuse-probe")
                                   for kind, query_id in ((1, 1), (28, 2))]
            (root / "assertions.jsonl").write_text("\n".join(json.dumps(sample) for sample in samples))
            for suffix, packets in (("before", 1), ("after", 5)):
                (root / f"tun.{suffix}.json").write_text(json.dumps([{"linkinfo": {"info_kind": "tun"}, "stats64": {"rx": {"packets": packets}}}]))
            def write_events():
                for role, entries in events.items():
                    (root / (role + ".jsonl")).write_text("\n".join(json.dumps(event) for event in entries))
            write_events()
            CHAIN.verify(root)
            events["dnsmasq"].append(dict(events["core1053"][0]))
            write_events()
            with self.assertRaises(AssertionError):
                CHAIN.verify(root)
            events["dnsmasq"].clear()
            events["core1053"][0]["peer"] = "10.204.0.1"
            write_events()
            with self.assertRaises(AssertionError):
                CHAIN.verify(root)
            events["core1053"][0]["peer"] = CHAIN.UPSTREAM
            events["adguard"][0]["id"] += 1
            with self.assertRaises(AssertionError):
                CHAIN.verify_events(samples, events)
            events["adguard"][0]["id"] -= 1
            old_connection = events["core1053"][1]["connection_id"]
            events["core1053"][1]["connection_id"] = "reuse-probe"
            with self.assertRaises(AssertionError):
                CHAIN.verify_events(samples, events)
            events["core1053"][1]["connection_id"] = old_connection
            events["upstream53"].append(dict(events["upstream53"][0]))
            with self.assertRaises(AssertionError):
                CHAIN.verify_events(samples, events)


if __name__ == "__main__":
    unittest.main()
