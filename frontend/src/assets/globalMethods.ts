import * as Vue from 'vue'

window.Vue = Vue

window.AsyncFunction = Object.getPrototypeOf(async function () {}).constructor
