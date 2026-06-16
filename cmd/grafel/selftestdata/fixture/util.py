# util.py is the second-language source in the embedded grafel selftest
# fixture (#5224). It gives the cold-index layer a multi-language corpus so
# the selftest exercises more than one extractor.
#
# Known entity the selftest may assert on: function `compute_total`.


def compute_total(values):
    """Return the sum of values. A deterministic, dependency-free helper."""
    total = 0
    for v in values:
        total += v
    return total


def describe_total(values):
    """Calls compute_total and formats the result."""
    return "total=%d" % compute_total(values)
