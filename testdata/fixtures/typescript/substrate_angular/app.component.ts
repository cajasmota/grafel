// Angular component fixture — proves import-resolution quality for the jsts
// substrate sniffer (issue #2850).  Hand-written; no node_modules.
import { Component, OnInit } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { UserService } from './user.service';
import { environment } from '../environments/environment';

const API_BASE = environment.apiUrl ?? 'https://api.example.com';
const TIMEOUT = process.env['NG_TIMEOUT'] ?? '5000';

@Component({
  selector: 'app-root',
  templateUrl: './app.component.html',
})
export class AppComponent implements OnInit {
  title = 'my-angular-app';

  constructor(private http: HttpClient, private userService: UserService) {}

  ngOnInit(): void {
    this.userService.getUsers().subscribe((users) => {
      console.log(users);
    });
  }
}
