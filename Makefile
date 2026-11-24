.PHONY: prepare build package

prepare:
	python3 scripts/tvos.py prepare

build:
	python3 scripts/tvos.py build

package:
	python3 scripts/tvos.py package
