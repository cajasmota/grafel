// Source: https://github.com/grails/grails-core (synthetic based on real Grails BootStrap patterns) | License: Apache-2.0

package myapp

import grails.boot.config.GrailsAutoConfiguration

class BootStrap {

    def init = { servletContext ->
        println "Initialising Grails application"
    }

    def destroy = {
        println "Destroying Grails application"
    }
}

class MyGrailsService {

    def serviceMethod() {
        return "service result"
    }

    void doSomething(String input) {
        println input
    }
}
