import{L as H,aA as J,N as K,aB as Y,Q as tt,aD as et,a as s,aj as w,P as at,m as it,az as rt,ao as st,w as ot,n as nt,F as lt}from"./MermaidBlock-v9AZetVK.js";import{p as ct}from"./chunk-4BX2VUAB-DQJWVZYF.js";import{p as dt}from"./wardley-L42UT6IY-DeEHcBkC.js";import"./flow-dag-DCy2bX_w.js";import{t as M,F as pt,u as gt}from"./cosmograph-B8NrsQII.js";import"./query-CCVlMDnp.js";import"./vendor-DPzr_TSd.js";import"./index-jTTuCf76.js";import"./radix-DaA5PXwi.js";var ht=lt.pie,D={sections:new Map,showData:!1},u=D.sections,C=D.showData,ut=structuredClone(ht),ft=s(()=>structuredClone(ut),"getConfig"),mt=s(()=>{u=new Map,C=D.showData,nt()},"clear"),vt=s(({label:t,value:a})=>{if(a<0)throw new Error(`"${t}" has invalid value: ${a}. Negative values are not allowed in pie charts. All slice values must be >= 0.`);u.has(t)||(u.set(t,a),w.debug(`added new section: ${t}, with value: ${a}`))},"addSection"),xt=s(()=>u,"getSections"),St=s(t=>{C=t},"setShowData"),wt=s(()=>C,"getShowData"),B={getConfig:ft,clear:mt,setDiagramTitle:et,getDiagramTitle:tt,setAccTitle:Y,getAccTitle:K,setAccDescription:J,getAccDescription:H,addSection:vt,getSections:xt,setShowData:St,getShowData:wt},Dt=s((t,a)=>{ct(t,a),a.setShowData(t.showData),t.sections.map(a.addSection)},"populateDb"),Ct={parse:s(async t=>{const a=await dt("pie",t);w.debug(a),Dt(a,B)},"parse")},$t=s(t=>`
  .pieCircle{
    stroke: ${t.pieStrokeColor};
    stroke-width : ${t.pieStrokeWidth};
    opacity : ${t.pieOpacity};
  }
  .pieOuterCircle{
    stroke: ${t.pieOuterStrokeColor};
    stroke-width: ${t.pieOuterStrokeWidth};
    fill: none;
  }
  .pieTitleText {
    text-anchor: middle;
    font-size: ${t.pieTitleTextSize};
    fill: ${t.pieTitleTextColor};
    font-family: ${t.fontFamily};
  }
  .slice {
    font-family: ${t.fontFamily};
    fill: ${t.pieSectionTextColor};
    font-size:${t.pieSectionTextSize};
    // fill: white;
  }
  .legend text {
    fill: ${t.pieLegendTextColor};
    font-family: ${t.fontFamily};
    font-size: ${t.pieLegendTextSize};
  }
`,"getStyles"),yt=$t,Tt=s(t=>{const a=[...t.values()].reduce((r,n)=>r+n,0),$=[...t.entries()].map(([r,n])=>({label:r,value:n})).filter(r=>r.value/a*100>=1);return gt().value(r=>r.value).sort(null)($)},"createPieArcs"),At=s((t,a,$,y)=>{var W;w.debug(`rendering pie chart
`+t);const r=y.db,n=at(),T=it(r.getConfig(),n.pie),A=40,o=18,p=4,c=450,d=c,f=rt(a),l=f.append("g");l.attr("transform","translate("+d/2+","+c/2+")");const{themeVariables:i}=n;let[b]=st(i.pieOuterStrokeWidth);b??(b=2);const E=T.textPosition,g=Math.min(d,c)/2-A,G=M().innerRadius(0).outerRadius(g),P=M().innerRadius(g*E).outerRadius(g*E);l.append("circle").attr("cx",0).attr("cy",0).attr("r",g+b/2).attr("class","pieOuterCircle");const h=r.getSections(),N=Tt(h),O=[i.pie1,i.pie2,i.pie3,i.pie4,i.pie5,i.pie6,i.pie7,i.pie8,i.pie9,i.pie10,i.pie11,i.pie12];let m=0;h.forEach(e=>{m+=e});const _=N.filter(e=>(e.data.value/m*100).toFixed(0)!=="0"),v=pt(O).domain([...h.keys()]);l.selectAll("mySlices").data(_).enter().append("path").attr("d",G).attr("fill",e=>v(e.data.label)).attr("class","pieCircle"),l.selectAll("mySlices").data(_).enter().append("text").text(e=>(e.data.value/m*100).toFixed(0)+"%").attr("transform",e=>"translate("+P.centroid(e)+")").style("text-anchor","middle").attr("class","slice");const I=l.append("text").text(r.getDiagramTitle()).attr("x",0).attr("y",-400/2).attr("class","pieTitleText"),k=[...h.entries()].map(([e,S])=>({label:e,value:S})),x=l.selectAll(".legend").data(k).enter().append("g").attr("class","legend").attr("transform",(e,S)=>{const L=o+p,X=L*k.length/2,Z=12*o,q=S*L-X;return"translate("+Z+","+q+")"});x.append("rect").attr("width",o).attr("height",o).style("fill",e=>v(e.label)).style("stroke",e=>v(e.label)),x.append("text").attr("x",o+p).attr("y",o-p).text(e=>r.getShowData()?`${e.label} [${e.value}]`:e.label);const U=Math.max(...x.selectAll("text").nodes().map(e=>(e==null?void 0:e.getBoundingClientRect().width)??0)),j=d+A+o+p+U,F=((W=I.node())==null?void 0:W.getBoundingClientRect().width)??0,Q=d/2-F/2,V=d/2+F/2,R=Math.min(0,Q),z=Math.max(j,V)-R;f.attr("viewBox",`${R} 0 ${z} ${c}`),ot(f,c,z,T.useMaxWidth)},"draw"),bt={draw:At},Gt={parser:Ct,db:B,renderer:bt,styles:yt};export{Gt as diagram};
//# sourceMappingURL=pieDiagram-4H26LBE5-DN1EvIKN.js.map
