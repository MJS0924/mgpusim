# M1 Analysis Package

Python analysis infrastructure for the M1 measurement dataset.

## Requirements

Python 3.9+. Install dependencies:

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r analysis/requirements.txt
```

## Usage

```bash
# Run proposition verification (from repo root)
python3 analysis/verify_m1.py

# Run tests
pytest analysis/tests/ -v
```

## Modules

- `load_data.py` — load 90 parquet files into a single DataFrame
- `metrics.py` — derived metric computation (L2HR, utilization, SCR)
- `propositions.py` — M1-P1~P6 verification functions
- `verify_m1.py` — entry point; writes results/m1/e1_verification.md
