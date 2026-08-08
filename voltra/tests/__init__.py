"""Marks tests as a package so `from tests.conftest import ...` resolves.

Without this, pytest puts tests/ itself on sys.path rather than its parent, so
`tests.conftest` is importable only when the current directory happens to be on
sys.path — true under `python -m pytest`, false under the `pytest` console
script that CI uses. That difference hid a real breakage locally.
"""
