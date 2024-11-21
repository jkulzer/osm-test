run:
	go fmt .
	# templ fmt *
	# templ generate
	go run .

perf-info:
	 go tool pprof -pdf  . cpuprofile > cpuprofile.pdf

android-install:
	fyne package -os android -appID com.example.myapp
	adb install
