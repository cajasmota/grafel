// Source: https://github.com/grails/grails-core (synthetic based on real Grails app entry-point patterns) | License: Apache-2.0

package myapp

import grails.boot.GrailsApp
import grails.boot.config.GrailsAutoConfiguration

@GrailsApplication
class Application extends GrailsAutoConfiguration {

    static void main(String[] args) {
        GrailsApp.run(Application, args)
    }

    void afterStart() {
        println "Application started successfully"
    }

    void doWithApplicationContext() {
        println "Spring ApplicationContext is ready"
    }
}
