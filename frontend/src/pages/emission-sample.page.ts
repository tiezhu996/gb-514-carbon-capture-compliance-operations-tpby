import { ChangeDetectorRef, Component, OnInit } from '@angular/core';
import { EntityPageComponent } from '../components/entity-page.component';
import { SampleRevisionsComponent } from '../components/common/sample-revisions.component';
import { EmissionSampleStore } from '../stores/emission-sample.store';
import { ENTITY_CONFIGS } from '../types/status';
import type { DomainRecord } from '../types/domain';

@Component({
  selector: 'app-emission-sample-page',
  standalone: true,
  imports: [EntityPageComponent, SampleRevisionsComponent],
  template: `<app-entity-page [config]="config" [store]="store"/>
    <app-sample-revisions [records]="records" (revised)="refresh()"/>`,
})
export class EmissionSamplePage implements OnInit {
  readonly config = ENTITY_CONFIGS[2];
  records: DomainRecord[] = [];

  constructor(readonly store: EmissionSampleStore, private readonly changeDetector: ChangeDetectorRef) {}

  ngOnInit(): void {
    this.store.state$.subscribe((state) => {
      this.records = state.items;
      this.changeDetector.detectChanges();
    });
  }

  async refresh(): Promise<void> { await this.store.load(this.config.path); }
}
