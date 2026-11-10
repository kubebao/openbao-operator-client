import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("architecture", Path(__file__).resolve().parents[1] / "check-architecture.py")
architecture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(architecture)


class ArchitectureTests(unittest.TestCase):
    def test_valid_directions(self):
        for layer, dependencies in architecture.LAYERS.items():
            imports = [architecture.MODULE + "/" + dependency for dependency in dependencies]
            package = {"ImportPath": architecture.MODULE + "/" + layer, "Imports": imports + ["time"]}
            self.assertEqual(architecture.violations(package), [])

    def test_kubernetes_and_network_are_forbidden_in_pure_packages(self):
        for layer in ["evidence", "diagnose"]:
            for imported in ["k8s.io/client-go/dynamic", "net/http", "os/exec", "database/sql"]:
                package = {"ImportPath": architecture.MODULE + "/" + layer, "Imports": [imported]}
                self.assertTrue(architecture.violations(package), (layer, imported))

    def test_reverse_direction_is_forbidden(self):
        for origin, destination in [("adapter", "collect"), ("evidence", "diagnose"), ("diagnose", "adapter")]:
            package = {"ImportPath": architecture.MODULE + "/" + origin, "Imports": [architecture.MODULE + "/" + destination]}
            self.assertTrue(architecture.violations(package))

    def test_consumer_and_operator_dependencies_are_forbidden(self):
        for prefix in architecture.FORBIDDEN:
            package = {"ImportPath": architecture.MODULE + "/collect", "Imports": [prefix + "/internal/reader"]}
            self.assertTrue(architecture.violations(package))

    def test_tests_and_transitive_dependencies_are_checked(self):
        for key in ["TestImports", "XTestImports", "Deps"]:
            package = {"ImportPath": architecture.MODULE + "/evidence", key: ["k8s.io/apimachinery/pkg/runtime"]}
            self.assertTrue(architecture.violations(package))
