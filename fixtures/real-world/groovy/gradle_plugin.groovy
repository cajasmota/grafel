// Source: https://github.com/gradle/gradle (synthetic based on real Gradle build script patterns) | License: Apache-2.0

apply plugin: 'java'
apply plugin: 'com.example.my-gradle-plugin'
apply plugin: 'groovy'

task clean {
    doLast {
        delete buildDir
    }
}

task compileGroovy(type: GroovyCompile) {
    source = sourceSets.main.groovy
    destinationDir = file("${buildDir}/classes")
}

task buildJar(type: Jar) {
    archiveName = 'my-app.jar'
    from sourceSets.main.output
}

task integrationTest(dependsOn: compileGroovy) {
    doLast {
        println "Running integration tests"
    }
}
